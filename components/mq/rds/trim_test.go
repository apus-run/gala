package rds

import (
	"context"
	"reflect"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestTrimProtectsEveryGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ids     []string
		read    int
		pending int // index retained by the second group; -1 acknowledges all
		want    int
	}{
		{"oldest pending", []string{"1-0", "2-0", "3-0"}, 3, 0, 0},
		{"numeric sequence order", []string{"1-2", "1-10", "1-11"}, 3, 1, 1},
		{"numeric timestamp order", []string{"2-0", "10-0", "11-0"}, 3, 1, 1},
		{"beyond float precision", []string{"9007199254740992-0", "9007199254740993-0", "9007199254740994-0"}, 3, 1, 1},
		{"max sequence", []string{"1-18446744073709551615"}, 1, -1, 1},
		{"max id", []string{"18446744073709551615-18446744073709551615"}, 1, -1, 1},
		{"unread suffix", []string{"1-0", "2-0", "3-0"}, 1, -1, 1},
		{"unread group", []string{"1-0", "2-0", "3-0"}, 0, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, topic := setup(t)
			ctx := context.Background()
			key := StreamKey(topic)
			for _, id := range tc.ids {
				if err := client.XAdd(ctx, &redis.XAddArgs{Stream: key, ID: id, Values: map[string]any{"body": id}}).Err(); err != nil {
					t.Fatal(err)
				}
			}
			for _, group := range []string{"workers", "other"} {
				if err := client.XGroupCreate(ctx, key, group, "0").Err(); err != nil {
					t.Fatal(err)
				}
				n := len(tc.ids)
				if group == "other" {
					n = tc.read
				}
				if n == 0 {
					continue
				}
				if err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: "owner", Streams: []string{key, ">"}, Count: int64(n), Block: -1}).Err(); err != nil {
					t.Fatal(err)
				}
				for i, id := range tc.ids[:n] {
					if group == "other" && i == tc.pending {
						continue
					}
					if err := client.XAck(ctx, key, group, id).Err(); err != nil {
						t.Fatal(err)
					}
				}
			}
			c := &Consumer{client: client, topic: topic, group: "workers"}
			for attempt := 0; attempt < 2; attempt++ {
				// Concurrent callers must collectively remove the prefix only once.
				type result struct {
					count int64
					err   error
				}
				results := make(chan result, 8)
				for range 8 {
					go func() { n, err := c.Trim(ctx); results <- result{n, err} }()
				}
				var removed int64
				for range 8 {
					r := <-results
					if r.err != nil {
						t.Fatal(r.err)
					}
					removed += r.count
				}
				want := tc.want
				if attempt > 0 {
					want = 0
				}
				if removed != int64(want) {
					t.Fatalf("trim: removed=%d want=%d", removed, want)
				}
				rows, err := client.XRange(ctx, key, "-", "+").Result()
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, row := range rows {
					got = append(got, row.ID)
					if row.Values["body"] != row.ID {
						t.Fatalf("body lost: %v", row)
					}
				}
				if len(got) != len(tc.ids[tc.want:]) || len(got) > 0 && !reflect.DeepEqual(got, tc.ids[tc.want:]) {
					t.Fatalf("remaining: %v", got)
				}
				if tc.pending >= 0 {
					p, err := client.XPending(ctx, key, "other").Result()
					if err != nil || p.Count != 1 || p.Lower != tc.ids[tc.pending] {
						t.Fatalf("pending changed: %v %v", p, err)
					}
					claimed, err := client.XClaim(ctx, &redis.XClaimArgs{Stream: key, Group: "other", Consumer: "replacement", Messages: []string{tc.ids[tc.pending]}}).Result()
					if err != nil || len(claimed) != 1 || claimed[0].Values["body"] != tc.ids[tc.pending] {
						t.Fatalf("cannot recover pending: %v %v", claimed, err)
					}
				}
			}
		})
	}
}

func TestTrimWithoutGroupsAndErrors(t *testing.T) {
	client, topic := setup(t)
	ctx := context.Background()
	key := StreamKey(topic)
	c := &Consumer{client: client, topic: topic}
	if _, err := c.Trim(ctx); err == nil {
		t.Fatal("missing stream must return error")
	}
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: key, ID: "1-0", Values: map[string]any{"body": "keep"}}).Err(); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Trim(ctx); err != nil || n != 0 {
		t.Fatalf("no groups: %d %v", n, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Trim(canceled); err == nil {
		t.Fatal("cancellation ignored")
	}
	if n, err := client.XLen(ctx, key).Result(); err != nil || n != 1 {
		t.Fatalf("message lost: %d %v", n, err)
	}
}
