package rds

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/apus-run/gala/components/mq"
)

type envelope struct {
	ID         string            `json:"id"`
	Body       []byte            `json:"body"`
	Tag        string            `json:"tag,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}
type prepared struct {
	topic, id, data string
	delay           int64
}

func topicKey(topic string) string { return "gala:mq:{" + hex.EncodeToString([]byte(topic)) + "}" }

// StreamKey names the retained stream. ACK does not delete entries, so other
// groups can still process them; retention must be managed by the application.
func StreamKey(topic string) string { return topicKey(topic) + ":stream" }
func DelayKey(topic string) string  { return topicKey(topic) + ":delay" }
func DeadLetterKey(topic, group string) string {
	return topicKey(topic) + ":dead:" + hex.EncodeToString([]byte(group))
}

func prepare(message *mq.Message) (prepared, error) {
	if message == nil || strings.TrimSpace(message.Topic) == "" || message.DeferDuration < 0 {
		return prepared{}, errors.New("redisstream: invalid message, topic or delay")
	}
	if message.PartitionKey != "" {
		return prepared{}, fmt.Errorf("redisstream: partition ordering: %w", mq.ErrUnsupported)
	}
	e := envelope{ID: rand.Text(), Body: bytes.Clone(message.Body), Tag: message.Tag, Properties: maps.Clone(message.Properties)}
	data, err := json.Marshal(e)
	if err != nil {
		return prepared{}, err
	}
	delay := int64(message.DeferDuration / time.Millisecond)
	if message.DeferDuration%time.Millisecond != 0 {
		delay++
	}
	return prepared{topic: message.Topic, id: e.ID, data: string(data), delay: delay}, nil
}
func decode(topic string, message map[string]any) (*mq.MessageExt, error) {
	data, ok := message["data"].(string)
	if !ok {
		return nil, errors.New("redisstream: missing message envelope")
	}
	var e envelope
	if err := json.Unmarshal([]byte(data), &e); err != nil {
		return nil, fmt.Errorf("redisstream: invalid envelope: %w", err)
	}
	if e.ID == "" {
		return nil, errors.New("redisstream: missing message identity")
	}
	return &mq.MessageExt{Message: mq.Message{Topic: topic, Body: e.Body, Tag: e.Tag, Properties: e.Properties}, MsgID: e.ID}, nil
}
