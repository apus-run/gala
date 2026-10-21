package rds

import (
	"context"
	"fmt"
)

// Stats separates delivered-but-unacknowledged messages from group lag. Lag is
// -1 when Redis cannot determine it. Delayed counts the shared topic schedule;
// DeadLetters counts only this group's dead-letter stream. This is not an atomic
// snapshot and does not register any global metrics or background collectors.
type Stats struct {
	Pending     int64
	Lag         int64
	Delayed     int64
	DeadLetters int64
}

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
func (c *Consumer) Trim(ctx context.Context) (int64, error) {
	return c.client.Eval(ctx, trim, []string{StreamKey(c.topic)}).Int64()
}
