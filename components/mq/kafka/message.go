package kafka

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/apus-run/gala/components/mq"
	"github.com/twmb/franz-go/pkg/kgo"
)

const tagHeader = "gala.mq.tag"

func toRecord(message *mq.Message) (*kgo.Record, error) {
	if message == nil || message.Topic == "" {
		return nil, errors.New("kafka: message and topic are required")
	}
	if message.DeferDuration != 0 {
		return nil, fmt.Errorf("kafka: delayed delivery: %w", mq.ErrUnsupported)
	}
	if _, exists := message.Properties[tagHeader]; exists {
		return nil, fmt.Errorf("kafka: reserved property %q", tagHeader)
	}
	record := &kgo.Record{Topic: message.Topic, Value: bytes.Clone(message.Body)}
	if message.PartitionKey != "" {
		record.Key = []byte(message.PartitionKey)
	}
	keys := make([]string, 0, len(message.Properties))
	for key := range message.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		record.Headers = append(record.Headers, kgo.RecordHeader{Key: key, Value: []byte(message.Properties[key])})
	}
	if message.Tag != "" {
		record.Headers = append(record.Headers, kgo.RecordHeader{Key: tagHeader, Value: []byte(message.Tag)})
	}
	return record, nil
}

func fromRecord(record *kgo.Record) *mq.MessageExt {
	message := &mq.MessageExt{Message: mq.Message{
		Topic: record.Topic, Body: bytes.Clone(record.Value), PartitionKey: string(record.Key),
		Properties: make(map[string]string, len(record.Headers)),
	}, MsgID: recordID(record)}
	for _, header := range record.Headers {
		if header.Key == tagHeader {
			message.Tag = string(header.Value)
		} else {
			message.Properties[header.Key] = string(header.Value)
		}
	}
	return message
}

func recordID(record *kgo.Record) string {
	return fmt.Sprintf("%s:%d:%d", record.Topic, record.Partition, record.Offset)
}

func response(record *kgo.Record, err error) mq.SendResponse {
	if err != nil || record == nil {
		return mq.SendResponse{}
	}
	return mq.SendResponse{MessageID: recordID(record), Offset: record.Offset}
}
