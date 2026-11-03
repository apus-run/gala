package rds

import (
	"context"
	"fmt"
)

// Stats separates delivered-but-unacknowledged messages from group lag. Lag
// is -1 when Redis cannot determine it. Delayed counts the shared topic
// schedule; DeadLetters counts only this group's dead-letter stream. This is
// not an atomic snapshot and does not register any global metrics or
// background collectors.
// Stats 区分"已投递未确认"与组积压。Redis 无法确定时 Lag 为 -1。
// Delayed 统计主题共享的延迟队列；DeadLetters 只统计本组的死信 stream。
// 它不是原子快照，也不注册任何全局指标或后台采集器。
type Stats struct {
	// Pending counts delivered-but-unacknowledged entries of the group
	// | Pending 统计该组已投递未确认的条目数。
	Pending int64
	// Lag counts entries not yet delivered to the group; -1 when unknown
	// | Lag 统计该组尚未投递的条目数；未知时为 -1。
	Lag int64
	// Delayed counts the topic's shared delayed schedule | Delayed 统计该主题共享的延迟队列长度。
	Delayed int64
	// DeadLetters counts this group's dead-letter stream | DeadLetters 统计本组死信 stream 的长度。
	DeadLetters int64
}

// Stats reads the group's pending, lag, delayed and dead-letter counters
// | Stats 读取该组的 pending、积压、延迟与死信计数。
func (c *Consumer) Stats(ctx context.Context) (Stats, error) {
	groups, err := c.client.XInfoGroups(ctx, StreamKey(c.topic)).Result()
	if err != nil {
		return Stats{}, err
	}
	var result Stats
	found := false
	for _, group := range groups {
		if group.Name == c.group {
			result.Pending = group.Pending
			result.Lag = group.Lag
			found = true
			break
		}
	}
	if !found {
		return Stats{}, fmt.Errorf("redisstream: consumer group %q does not exist", c.group)
	}
	result.Delayed, err = c.client.ZCard(ctx, DelayKey(c.topic)).Result()
	if err != nil {
		return Stats{}, err
	}
	result.DeadLetters, err = c.client.XLen(ctx, DeadLetterKey(c.topic, c.group)).Result()
	if err != nil {
		return Stats{}, err
	}
	return result, nil
}

// Trim atomically deletes the prefix acknowledged by every existing group.
// Groups created later cannot replay trimmed history. No groups means no trim.
// Redis errors are returned; an uncertain reply may follow a successful trim.
// Trim 原子地删除已被所有现存组确认的前缀历史。之后新建的组无法重放被裁剪的
// 历史。没有组则不裁剪。Redis 错误原样返回；裁剪成功后也可能出现不确定应答。
func (c *Consumer) Trim(ctx context.Context) (int64, error) {
	return c.client.Eval(ctx, trim, []string{StreamKey(c.topic)}).Int64()
}
