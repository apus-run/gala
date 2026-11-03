package mq

import (
	"time"
)

// Message is a message to publish | Message 是待发布的消息.
type Message struct {
	// Topic is the destination topic | 目标主题
	Topic string
	// Body is the raw payload | 原始消息体
	Body []byte
	// Tag filters messages on the consumer side | 供消费端过滤的标签
	Tag string
	// PartitionKey routes messages with the same key to one partition, for ordering
	// | 分区键：相同键的路由到同一分区以保序
	PartitionKey string
	// Properties are user-defined headers | 用户自定义属性
	Properties map[string]string
	// DeferDuration delays delivery, when the backend supports it | 延迟投递时长，以后端支持为准
	DeferDuration time.Duration
}

// NewMessage creates a Message with topic and body | NewMessage 创建带主题与消息体的 Message.
func NewMessage(topic string, body []byte) *Message {
	return &Message{
		Topic: topic,
		Body:  body,
	}
}

// NewOrderlyMessage creates a Message pinned to a partition key | NewOrderlyMessage 创建绑定分区键的顺序消息.
func NewOrderlyMessage(topic, partitionKey string, body []byte) *Message {
	return NewMessage(topic, body).WithPartitionKey(partitionKey)
}

// NewDeferMessage creates a Message delivered after deferDuration | NewDeferMessage 创建延迟 deferDuration 投递的消息.
func NewDeferMessage(topic string, deferDuration time.Duration, body []byte) *Message {
	return NewMessage(topic, body).WithDeferDuration(deferDuration)
}

// WithTag sets the tag and returns the message | WithTag 设置标签并返回消息.
func (m *Message) WithTag(tag string) *Message {
	m.Tag = tag
	return m
}

// WithPartitionKey sets the partition key and returns the message | WithPartitionKey 设置分区键并返回消息.
func (m *Message) WithPartitionKey(partitionKey string) *Message {
	m.PartitionKey = partitionKey
	return m
}

// WithProperties sets the properties and returns the message | WithProperties 设置属性并返回消息.
func (m *Message) WithProperties(properties map[string]string) *Message {
	m.Properties = properties
	return m
}

// WithDeferDuration sets the defer duration and returns the message | WithDeferDuration 设置延迟时长并返回消息.
func (m *Message) WithDeferDuration(deferDuration time.Duration) *Message {
	m.DeferDuration = deferDuration
	return m
}

// MessageExt is a delivered message carrying its broker-assigned id | MessageExt 是投递到的消息，附 broker 分配的 id.
type MessageExt struct {
	Message
	// MsgID is the id assigned by the broker | broker 分配的消息 id
	MsgID string
}
