// Package option provides generic building blocks for the functional option
// pattern | 函数式 Option 模式的泛型积木.
//
// # Notes | 说明
//
// Option[T] is a plain func(*T) and Apply runs the options in order; T is
// normally a configuration struct. When an option must validate, OptionErr[T]
// returns an error and ApplyErr stops at the first failure. Option[T].Err()
// adapts a non-validating option so both kinds share one list.
// Option[T] 就是普通的 func(*T)，Apply 按顺序应用；T 通常是配置结构体。
// 需要校验时用 OptionErr[T]（返回 error），ApplyErr 在第一个错误处中断；
// Option[T].Err() 把不带校验的 option 适配成 OptionErr，两种可混在同一个
// 列表里。
//
// # Features | 功能
//
// - Option[T]/Apply for plain options | 普通选项与应用
// - OptionErr[T]/ApplyErr stop at the first error | 校验型选项，首个错误即中断
// - Option[T].Err() mixes both kinds | 两种选项混用
//
// # Example | 示例
//
//	u := &User{}
//	option.Apply(u, WithName("Tom")) // WithName is option.Option[User]
//	err := option.ApplyErr(u, WithName("Ann").Err(), WithAgeErr(18))
package option

// Option is the generic design for the option pattern, so that code does not
// need to define many similar option structs of its own. T is normally a struct.
// Option 是用于 Option 模式的泛型设计，
// 避免在代码中定义很多类似这样的结构体；
// 一般情况下 T 应该是一个结构体。
type Option[T any] func(t *T)

// Apply applies opts to t in order | Apply 将 opts 依次应用在 t 之上.
func Apply[T any](t *T, opts ...Option[T]) {
	for _, opt := range opts {
		opt(t)
	}
}

// OptionErr is like Option but returns an error.
// Prefer Option unless the option pattern you are designing needs validation.
// OptionErr 形如 Option，但是会返回一个 error；
// 你应该优先使用 Option，除非你在设计 option 模式的时候需要进行一些校验。
type OptionErr[T any] func(t *T) error

// Err adapts an Option to an OptionErr, for lists mixing both kinds:
// Err 将 Option 适配为 OptionErr，用于混用两种 option 的列表：
//
//	ApplyErr[User](u, WithName("Tom").Err(), WithAgeErr(18))
func (o Option[T]) Err() OptionErr[T] {
	return func(t *T) error {
		o(t)
		return nil
	}
}

// ApplyErr is like Apply: it applies opts to t, and if any opt returns an
// error, it stops and returns that error.
// ApplyErr 形如 Apply，它将 opts 应用在 t 之上，
// 如果 opts 中任何一个返回 error，那么它会中断并且返回 error。
func ApplyErr[T any](t *T, opts ...OptionErr[T]) error {
	for _, opt := range opts {
		if err := opt(t); err != nil {
			return err
		}
	}
	return nil
}
