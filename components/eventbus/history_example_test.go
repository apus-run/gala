package eventbus_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/apus-run/gala/components/eventbus"
)

func ExampleHistoryRecorder_WrapPublisher() {
	bus := eventbus.NewEventBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer bus.Close()
	history := eventbus.NewHistoryRecorder(2)
	publisher := history.WrapPublisher(bus)
	for _, id := range []string{"first", "second", "third"} {
		if err := publisher.Publish(context.Background(), &eventbus.Event{ID: id, Type: "changed"}); err != nil {
			panic(err)
		}
	}
	for _, event := range history.Snapshot() {
		fmt.Println(event.ID)
	}
	// Output:
	// second
	// third
}
