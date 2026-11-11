package backoff

import (
	"context"
	"testing"
	"time"
)

func TestDelay(t *testing.T) {
	var d Delay
	for _, ceiling := range []time.Duration{1, 2, 4, 8, 10, 10} {
		want := ceiling * time.Second
		got := d.Next(time.Second, 10*time.Second)
		if time.Duration(d) != want || got < want/2 || got > want {
			t.Fatalf("ceiling %v, delay %v", d, got)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if d.Wait(ctx) {
		t.Fatal("continued after cancellation")
	}
}
