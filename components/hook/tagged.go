package hook

import "slices"

// Tagger defines an interface for event data structs that support tags/groups/categories/etc.
// Usually used together with TaggedHook.
type Tagger interface {
	Resolver

	Tags() []string
}

// wrapped local Hook embedded struct to limit the public API surface.
type mainHook[T Tagger] struct {
	*Hook[T]
}

// NewTaggedHook creates a new TaggedHook with the provided main hook and optional tags.
//
// The tags are copied, so later changes of the passed slice are not reflected.
func NewTaggedHook[T Tagger](hook *Hook[T], tags ...string) *TaggedHook[T] {
	return &TaggedHook[T]{
		mainHook[T]{hook},
		slices.Clone(tags),
	}
}

// TaggedHook defines a proxy hook which register handlers that are triggered only
// if the TaggedHook.tags are empty or includes at least one of the event data tag(s).
//
// A handler that doesn't match the event tags is skipped, but the chain
// continues with the next handler.
type TaggedHook[T Tagger] struct {
	mainHook[T]

	tags []string
}

// CanTriggerOn checks if the current TaggedHook can be triggered with
// the provided event data tags.
//
// It returns always true if the hook doesn't have any tags.
func (h *TaggedHook[T]) CanTriggerOn(tagsToCheck []string) bool {
	if len(h.tags) == 0 {
		return true // match all
	}

	for _, t := range tagsToCheck {
		if slices.Contains(h.tags, t) {
			return true
		}
	}

	return false
}

// Bind registers the provided handler to the current hooks queue.
//
// It is similar to [Hook.Bind] with the difference that the handler
// function is invoked only if the event data tags satisfy h.CanTriggerOn.
//
// The passed handler is not modified (except for Id, if it was empty),
// so the same handler can be safely bound to multiple tagged hooks.
func (h *TaggedHook[T]) Bind(handler *Handler[T]) string {
	if handler == nil || handler.Func == nil {
		panic(errNilHandler)
	}

	wrapped := *handler
	wrapped.Func = h.wrap(handler.Func)

	handler.Id = h.mainHook.Bind(&wrapped)

	return handler.Id
}

// BindFunc registers a new handler with the specified function.
//
// It is similar to [Hook.Bind] with the difference that the handler
// function is invoked only if the event data tags satisfy h.CanTriggerOn.
func (h *TaggedHook[T]) BindFunc(fn func(e T) error) string {
	return h.Bind(&Handler[T]{Func: fn})
}

// wrap returns a handler func that runs fn only for the matching events.
func (h *TaggedHook[T]) wrap(fn func(T) error) func(T) error {
	return func(e T) error {
		// check len first to avoid calling e.Tags() (that may allocate)
		// when the hook matches every event
		if len(h.tags) == 0 || h.CanTriggerOn(e.Tags()) {
			return fn(e)
		}

		return e.Next()
	}
}
