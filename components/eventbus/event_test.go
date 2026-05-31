package eventbus

import "testing"

type clonePayload struct {
	Labels map[string]string
	Values []int
	Child  *clonePayload
}

func TestEventClone(t *testing.T) {
	data := &clonePayload{
		Labels: map[string]string{"key": "value"},
		Values: []int{1, 2, 3},
	}
	event := NewEvent("test", data).WithMetadata("source", "original")

	cloned := event.Clone()
	clonedData := cloned.Data.(*clonePayload)

	if cloned == event {
		t.Fatal("Clone returned the original event")
	}
	if clonedData != data {
		t.Fatal("Clone copied the data pointer")
	}

	clonedData.Labels["key"] = "cloned"
	clonedData.Values[0] = 9
	cloned.Metadata["source"] = "cloned"

	if data.Labels["key"] != "cloned" {
		t.Fatal("Clone did not share the original data map")
	}
	if data.Values[0] != 9 {
		t.Fatal("Clone did not share the original data slice")
	}
	if event.Metadata["source"] != "cloned" {
		t.Fatal("Clone did not share the original metadata")
	}
}

func TestEventCloneNil(t *testing.T) {
	var event *Event
	if event.Clone() != nil {
		t.Fatal("Clone returned a non-nil event for a nil receiver")
	}
}

func TestNewEventIDsAreUnique(t *testing.T) {
	ids := make(map[string]struct{})
	for range 1000 {
		id := NewEvent("test", nil).ID
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate event ID: %s", id)
		}
		ids[id] = struct{}{}
	}
}
