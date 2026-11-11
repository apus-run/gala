//go:build integration

package etcd_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	conf "github.com/apus-run/gala/components/conf/v2"
	"github.com/apus-run/gala/components/conf/v2/sources/etcd"
	"github.com/apus-run/gala/components/conf/v2/sources/file"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func liveClient(t *testing.T, env string) *clientv3.Client {
	t.Helper()
	endpoint := os.Getenv(env)
	if endpoint == "" {
		t.Fatalf("missing %s; run testdata/ci/integration.sh", env)
	}
	c, err := clientv3.New(clientv3.Config{Endpoints: []string{endpoint}, DialTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func put(t *testing.T, ctx context.Context, c *clientv3.Client, key, value string) {
	t.Helper()
	if _, err := c.Put(ctx, key, value); err != nil {
		t.Fatal(err)
	}
}
func awaitSignal(t *testing.T, ctx context.Context, ch <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("watch closed")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func TestIntegrationDocument(t *testing.T) { testIntegrationMode(t, false) }
func TestIntegrationPrefix(t *testing.T)   { testIntegrationMode(t, true) }
func testIntegrationMode(t *testing.T, prefix bool) {
	c := liveClient(t, "CONF_TEST_ETCD")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	key := "conf-test/" + t.Name()
	s := etcd.New(c, key, file.JSON)
	var first, next string
	if prefix {
		s = etcd.NewPrefix(c, key)
		put(t, ctx, c, key+"/Other", "4")
		key += "/Port"
		first, next = "1", "2"
	} else {
		first, next = `{"Port":1}`, `{"Port":2}`
	}
	put(t, ctx, c, key, first)
	cfg, err := conf.Open(ctx, struct{ Port, Other int }{}, s)
	if err != nil || cfg.Current().Port != 1 {
		t.Fatal(err)
	}
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, ch)
	put(t, ctx, c, key, next)
	awaitSignal(t, ctx, ch)
	if err := cfg.Reload(ctx); err != nil || cfg.Current().Port != 2 {
		t.Fatal(err)
	}
	if _, err := c.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, ch)
	if prefix {
		if err := cfg.Reload(ctx); err != nil || cfg.Current().Port != 0 || cfg.Current().Other != 4 {
			t.Fatal(err)
		}
		if _, err := c.Delete(ctx, strings.TrimSuffix(key, "Port")+"Other"); err != nil {
			t.Fatal(err)
		}
		awaitSignal(t, ctx, ch)
	}
	old := cfg.Current()
	if err := cfg.Reload(ctx); !errors.Is(err, fs.ErrNotExist) || cfg.Current() != old {
		t.Fatal(err)
	}
}
func TestIntegrationLeaderRecovery(t *testing.T) {
	minority := liveClient(t, "CONF_TEST_ETCD")
	majority := liveClient(t, "CONF_TEST_ETCD_MAJORITY")
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	key := "conf-test/partition"
	put(t, ctx, majority, key, `{"N":1}`)
	s := etcd.New(minority, key, file.JSON)
	c, err := conf.Open(ctx, struct{ N int }{}, s)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	awaitSignal(t, ctx, ch)
	node, network := os.Getenv("CONF_TEST_ETCD_NODE"), os.Getenv("CONF_TEST_ETCD_PEERS")
	if !strings.HasPrefix(node, "gala-conf-") || network != strings.TrimSuffix(node, "-1")+"-peers" {
		t.Fatal("missing isolated test cluster")
	}
	docker := func(args ...string) error {
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Log(string(out))
		}
		return err
	}
	if err := docker("network", "disconnect", network, node); err != nil {
		t.Fatal(err)
	}
	reconnect := func() error {
		return docker("network", "connect", "--ip", os.Getenv("CONF_TEST_ETCD_PEER_IP"), "--alias", "n1", network, node)
	}
	connected := false
	defer func() {
		if !connected {
			_ = reconnect()
		}
	}()
	// Confirm the isolated endpoint can no longer complete a quorum read.
	for {
		readCtx, stop := context.WithTimeout(ctx, 400*time.Millisecond)
		_, err := minority.Get(readCtx, key)
		stop()
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	// Wait for the two connected members to elect a leader before the one
	// write under test. Retrying writes here would mask missed invalidations.
	for {
		readCtx, stop := context.WithTimeout(ctx, time.Second)
		_, err := majority.Get(readCtx, key)
		stop()
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	put(t, ctx, majority, key, `{"N":2}`)
	if err := reconnect(); err != nil {
		t.Fatal(err)
	}
	connected = true
	awaitSignal(t, ctx, ch)
	// A confirmation can arrive before the isolated member is ready for reads.
	for {
		if err := c.Reload(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if c.Current().N != 2 {
		t.Fatalf("stale snapshot: %+v", c.Current())
	}
}
