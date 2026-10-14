package nsq

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apus-run/gala/components/mq"
	nsqclient "github.com/nsqio/go-nsq"
)

type published struct {
	kind, topic string
	bodies      [][]byte
	delay       time.Duration
	ack         chan *nsqclient.ProducerTransaction
}
type fakeProducer struct {
	calls chan published
	err   error
	stops atomic.Int32
}

func (f *fakeProducer) PublishAsync(topic string, body []byte, ack chan *nsqclient.ProducerTransaction, _ ...interface{}) error {
	if f.err != nil {
		return f.err
	}
	f.calls <- published{kind: "PUB", topic: topic, bodies: [][]byte{body}, ack: ack}
	return nil
}
func (f *fakeProducer) MultiPublishAsync(topic string, bodies [][]byte, ack chan *nsqclient.ProducerTransaction, _ ...interface{}) error {
	if f.err != nil {
		return f.err
	}
	f.calls <- published{kind: "MPUB", topic: topic, bodies: bodies, ack: ack}
	return nil
}
func (f *fakeProducer) DeferredPublishAsync(topic string, delay time.Duration, body []byte, ack chan *nsqclient.ProducerTransaction, _ ...interface{}) error {
	if f.err != nil {
		return f.err
	}
	f.calls <- published{kind: "DPUB", topic: topic, delay: delay, bodies: [][]byte{body}, ack: ack}
	return nil
}
func (f *fakeProducer) Stop() { f.stops.Add(1) }
func testProducer() (*Producer, *fakeProducer) {
	client := &fakeProducer{calls: make(chan published, maxPendingSends+1)}
	return &Producer{clients: []producerClient{client}, slots: make(chan struct{}, maxPendingSends), stop: make(chan struct{})}, client
}

func TestPublishCommandsAndSnapshot(t *testing.T) {
	for _, mode := range []string{"PUB", "MPUB", "DPUB"} {
		t.Run(mode, func(t *testing.T) {
			p, client := testProducer()
			defer p.Close()
			message := mq.NewMessage("events", []byte("body"))
			messages := []*mq.Message{message}
			if mode == "MPUB" {
				messages = append(messages, message)
			}
			if mode == "DPUB" {
				message.DeferDuration = time.Millisecond + time.Nanosecond
			}
			done, err := p.send(context.Background(), messages, nil)
			if err != nil {
				t.Fatal(err)
			}
			call := <-client.calls
			message.Body[0] = 'X'
			if call.kind != mode || call.topic != "events" {
				t.Fatalf("call=%+v", call)
			}
			for _, body := range call.bodies {
				if string(body) != "body" {
					t.Fatalf("body mutated: %s", body)
				}
			}
			if mode == "DPUB" && call.delay != 2*time.Millisecond {
				t.Fatalf("delay=%v", call.delay)
			}
			call.ack <- &nsqclient.ProducerTransaction{}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPublishErrorAndCallback(t *testing.T) {
	p, client := testProducer()
	defer p.Close()
	failure := errors.New("publish failed")
	ctx := context.Background()
	callback := make(chan error, 2)
	if err := p.SendAsync(ctx, func(got context.Context, response mq.SendResponse, err error) {
		if got != ctx || response != (mq.SendResponse{}) {
			callback <- errors.New("callback metadata changed")
		} else {
			callback <- err
		}
	}, mq.NewMessage("events", nil)); err != nil {
		t.Fatal(err)
	}
	call := <-client.calls
	call.ack <- &nsqclient.ProducerTransaction{Error: failure}
	if err := <-callback; !errors.Is(err, failure) {
		t.Fatal(err)
	}
	p.Close()
	select {
	case <-callback:
		t.Fatal("duplicate callback")
	default:
	}
	client.err = failure
	p2 := &Producer{clients: []producerClient{client}, slots: make(chan struct{}, 1), stop: make(chan struct{})}
	defer p2.Close()
	if err := p2.SendAsync(ctx, func(context.Context, mq.SendResponse, error) { t.Error("callback on rejected enqueue") }, mq.NewMessage("events", nil)); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if len(p2.slots) != 0 {
		t.Fatal("rejected send leaked slot")
	}
}

func TestCanceledSendKeepsSlotUntilAck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, client := testProducer()
		defer p.Close()
		p.slots = make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		done, err := p.send(ctx, []*mq.Message{mq.NewMessage("events", nil)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		call := <-client.calls
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		synctest.Wait()
		if _, err := p.send(context.Background(), []*mq.Message{mq.NewMessage("events", nil)}, nil); !errors.Is(err, ErrBackpressure) {
			t.Fatalf("unacknowledged send lost its slot: %v", err)
		}
		call.ack <- &nsqclient.ProducerTransaction{}
		synctest.Wait()
		if len(p.slots) != 0 {
			t.Fatal("ack did not release slot")
		}
	})
}

func TestCloseUnblocksPendingSends(t *testing.T) {
	p, client := testProducer()
	callback := make(chan error, 1)
	if err := p.SendAsync(context.Background(), func(_ context.Context, _ mq.SendResponse, err error) { callback <- err }, mq.NewMessage("events", nil)); err != nil {
		t.Fatal(err)
	}
	call := <-client.calls
	for range 2 {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-callback; !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if client.stops.Load() != 1 || len(p.slots) != 0 {
		t.Fatal("shutdown did not drain operations once")
	}
	// The SDK may deliver after cancellation/close. It must never block its router.
	select {
	case call.ack <- &nsqclient.ProducerTransaction{}:
	default:
		t.Fatal("late SDK completion would block")
	}
	if _, err := p.Send(context.Background(), mq.NewMessage("events", nil)); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestPublishValidation(t *testing.T) {
	for _, message := range []*mq.Message{nil, {}, mq.NewMessage("events", nil).WithTag("tag"), mq.NewMessage("events", nil).WithProperties(map[string]string{"a": "b"}), mq.NewOrderlyMessage("events", "key", nil), mq.NewDeferMessage("events", -1, nil)} {
		p, client := testProducer()
		if _, err := p.SendBatch(context.Background(), []*mq.Message{mq.NewMessage("events", nil), message}); err == nil {
			t.Error("invalid batch accepted")
		}
		if len(client.calls) != 0 {
			t.Error("invalid batch partially sent")
		}
		p.Close()
	}
	p, client := testProducer()
	defer p.Close()
	for _, messages := range [][]*mq.Message{nil, {mq.NewMessage("a", nil), mq.NewMessage("b", nil)}, {mq.NewMessage("a", nil), mq.NewDeferMessage("a", time.Second, nil)}, {mq.NewDeferMessage("a", time.Duration(math.MaxInt64), nil)}} {
		if _, err := p.SendBatch(context.Background(), messages); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Send(ctx, mq.NewMessage("events", nil)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := p.SendAsync(context.Background(), nil, mq.NewMessage("events", nil)); err == nil {
		t.Fatal("nil callback accepted")
	}
	if len(client.calls) != 0 {
		t.Fatal("validation reached SDK")
	}
}
