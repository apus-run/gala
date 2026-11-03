package hook

import "slices"

// Tagger defines an interface for event data structs that support tags/groups/categories/etc.
// Usually used together with TaggedHook.
// Tagger 定义支持标签/分组/类别等的事件数据结构接口，通常与 TaggedHook 搭配使用。
type Tagger interface {
	Resolver

	Tags() []string
}

// wrapped local Hook embedded struct to limit the public API surface.
// 内嵌的本地 Hook 包装结构，用于限制公开 API 面。
type mainHook[T Tagger] struct {
	*Hook[T]
}

// NewTaggedHook creates a new TaggedHook with the provided main hook and optional tags.
//
// The tags are copied, so later changes of the passed slice are not reflected.
// NewTaggedHook 用给定主钩子和可选标签创建新的 TaggedHook。
// 标签会被复制，之后修改传入切片不会影响钩子。
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
// TaggedHook 定义代理钩子：其注册的处理器只在 tags 为空或与事件数据的
// 至少一个标签相交时才触发。不匹配事件标签的处理器被跳过，但链条继续
// 执行下一个处理器。
type TaggedHook[T Tagger] struct {
	mainHook[T]

	tags []string
}

// CanTriggerOn checks if the current TaggedHook can be triggered with
// the provided event data tags.
//
// It returns always true if the hook doesn't have any tags.
// CanTriggerOn 检查当前 TaggedHook 能否以给定事件数据标签触发。
// 钩子没有任何标签时始终返回 true。
func (h *TaggedHook[T]) CanTriggerOn(tagsToCheck []string) bool {
	if len(h.tags) == 0 {
		return true // match all | 匹配所有事件
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
// Bind 把给定处理器注册到当前钩子队列。与 [Hook.Bind] 的区别在于：仅当事件
// 数据标签满足 h.CanTriggerOn 时才调用处理函数。传入的处理器不被修改
// （Id 为空时除外），因此同一处理器可安全绑定到多个 tagged 钩子。
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
// BindFunc 用指定函数注册新处理器。与 [Hook.Bind] 的区别在于：仅当事件数据
// 标签满足 h.CanTriggerOn 时才调用处理函数。
func (h *TaggedHook[T]) BindFunc(fn func(e T) error) string {
	return h.Bind(&Handler[T]{Func: fn})
}

// wrap returns a handler func that runs fn only for the matching events.
// wrap 返回只在匹配事件上运行 fn 的处理函数。
func (h *TaggedHook[T]) wrap(fn func(T) error) func(T) error {
	return func(e T) error {
		// check len first to avoid calling e.Tags() (that may allocate)
		// when the hook matches every event
		// 先检查长度，避免在钩子匹配所有事件时调用 e.Tags()（可能引起分配）
		if len(h.tags) == 0 || h.CanTriggerOn(e.Tags()) {
			return fn(e)
		}

		return e.Next()
	}
}
