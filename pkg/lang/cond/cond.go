// Package cond helps users choose values according to various conditions in
// one line.
// 包 cond 帮助用户在一行内按各种条件选择值。
package cond

import "slices"

// If returns onTrue when cond is true, otherwise returns onFalse.
// It is used as a replacement of ternary conditional operator (:?) in many
// other programming languages.
// If 在 cond 为真时返回 onTrue，否则返回 onFalse，
// 可替代许多其他语言中的三目运算符（:?)。
//
// ⚠️ WARNING: onTrue and onFalse always be evaluated regardless of the truth
// of cond. Use [IfLazy], [IfLazyL], and [IfLazyR] if you need lazy evaluation.
// ⚠️ 警告：无论 cond 真假，onTrue 与 onFalse 都会被求值；
// 需要惰性求值请使用 [IfLazy]、[IfLazyL] 和 [IfLazyR]。
//
// 🚀 EXAMPLE:
//
//	If(true, 1, 2)                       ⏩ 1
//	If(false, 1, 2)                      ⏩ 2
//	If(p != nil, p.foo, nil)             ⏩ ❌PANIC❌
//	If(true, 1, default())               ⏩ 1 // ⚠️ but func default is always evaluated
func If[T any](cond bool, onTrue, onFalse T) T {
	if cond {
		return onTrue
	} else {
		return onFalse
	}
}

// Lazy is a value type that evaluates only when needed.
// Lazy 是只在需要时才求值的值类型。
type Lazy[T any] func() T

// IfLazy is a variant of [If], accepts [Lazy] values.
// IfLazy 是 [If] 的变体，接受 [Lazy] 值。
//
// 🚀 EXAMPLE:
//
//	v1 := func() int {return 1}
//	v2 := func() int {return 2}
//	vp := func () int { panic("") }
//	IfLazy(true, v1, v2)   ⏩ 1
//	IfLazy(false, v1, v2)  ⏩ 2
//	IfLazy(true, v1, vp)   ⏩ 1 // won't panic
//	IfLazy(false, vp, v2)  ⏩ 2 // won't panic
func IfLazy[T any](cond bool, onTrue, onFalse Lazy[T]) T {
	if cond {
		return onTrue()
	} else {
		return onFalse()
	}
}

// IfLazyL is a variant of [If], accepts [Lazy] onTrue value.
// IfLazyL 是 [If] 的变体，onTrue 为 [Lazy] 值。
func IfLazyL[T any](cond bool, onTrue Lazy[T], onFalse T) T {
	if cond {
		return onTrue()
	} else {
		return onFalse
	}
}

// IfLazyR is a variant of [If], accepts [Lazy] onFalse value.
// IfLazyR 是 [If] 的变体，onFalse 为 [Lazy] 值。
func IfLazyR[T any](cond bool, onTrue T, onFalse Lazy[T]) T {
	if cond {
		return onTrue
	} else {
		return onFalse()
	}
}

type switchBuilder[R any, T comparable] struct {
	variable T
	matched  bool
	result   R
}

type whenClause[R any, T comparable] struct {
	parent  *switchBuilder[R, T]
	matched bool
}

// Switch initiates a new switchBuilder with the given variable.
// It is used as a more flexible alternative to the built-in switch statement.
// Switch 以给定变量初始化一个新的 switchBuilder，
// 用作内置 switch 语句更灵活的替代品。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).Case(1, "One").Default("Other")	⏩ "One"
//	Switch[string](2).Case(1, "One").Default("Other")	⏩ "Other"
func Switch[R any, T comparable](variable T) *switchBuilder[R, T] {
	return &switchBuilder[R, T]{
		variable: variable,
		matched:  false,
	}
}

// Case adds a case to the switch statement. If the case matches and no
// previous case has matched, it sets the result.
// Case 向 switch 语句添加一个分支：若该分支匹配且之前没有分支命中，则设置结果。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).Case(1, "One").Default("Other")	⏩ "One"
//	Switch[string](2).Case(1, "One").Default("Other")	⏩ "Other"
func (s *switchBuilder[R, T]) Case(value T, result R) *switchBuilder[R, T] {
	if !s.matched && s.variable == value {
		s.matched = true
		s.result = result
	}
	return s
}

// CaseLazy is a variant of Case that accepts a Lazy result.
// The result function is only called if the case matches.
// CaseLazy 是 Case 的变体，接受 Lazy 结果；
// 结果函数仅在分支匹配时才被调用。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).CaseLazy(1, func() string { return "One" }).Default("Other")	⏩ "One"
//	Switch[string](2).CaseLazy(1, func() string { return "One" }).Default("Other")	⏩ "Other"
func (s *switchBuilder[R, T]) CaseLazy(value T, resultF Lazy[R]) *switchBuilder[R, T] {
	if !s.matched && s.variable == value {
		s.matched = true
		s.result = resultF()
	}
	return s
}

// When initiates a multi-value case statement. It returns a whenClause
// which must be followed by a Then or ThenLazy call.
// When 发起一个多值分支，返回必须随后调用 Then 或 ThenLazy 的 whenClause。
//
// ⚠️ WARNING: When must be followed by a Then or ThenLazy call, otherwise
// the behavior is undefined.
// ⚠️ 警告：When 之后必须调用 Then 或 ThenLazy，否则行为未定义。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).When(1, 2).Then("One").Default("Other")	⏩ "One"
//	Switch[string](2).When(1, 2).Then("One").Default("Other")	⏩ "One"
//	Switch[string](3).When(1, 2).Then("One").Default("Other")	⏩ "Other"
func (s *switchBuilder[R, T]) When(values ...T) *whenClause[R, T] {
	wc := &whenClause[R, T]{
		parent:  s,
		matched: false,
	}
	if !s.matched {
		if slices.Contains(values, s.variable) {
			wc.matched = true
		}
	}
	return wc
}

// Then sets the result for a When clause if it matches and no previous case
// has matched.
// Then 在 When 分支匹配且之前没有分支命中时设置结果。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).When(1, 2).Then("One").Default("Other")	⏩ "One"
//	Switch[string](2).When(1, 2).Then("One").Default("Other")	⏩ "One"
//	Switch[string](3).When(1, 2).Then("One").Default("Other")	⏩ "Other"
func (wc *whenClause[R, T]) Then(result R) *switchBuilder[R, T] {
	if !wc.parent.matched && wc.matched {
		wc.parent.matched = true
		wc.parent.result = result
	}
	return wc.parent
}

// ThenLazy is a variant of Then that accepts a lazy result function.
// The function is only called if the When clause matches and no previous
// case has matched.
// ThenLazy 是 Then 的变体，接受惰性结果函数；
// 仅在 When 分支匹配且之前没有分支命中时才调用该函数。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).When(1, 2).ThenLazy(func() string { return "One" }).Default("Other")	⏩ "One"
//	Switch[string](2).When(1, 2).ThenLazy(func() string { return "One" }).Default("Other")	⏩ "One"
//	Switch[string](3).When(1, 2).ThenLazy(func() string { return "One" }).Default("Other")	⏩ "Other"
func (wc *whenClause[R, T]) ThenLazy(resultF func() R) *switchBuilder[R, T] {
	if !wc.parent.matched && wc.matched {
		wc.parent.matched = true
		wc.parent.result = resultF()
	}
	return wc.parent
}

// Default sets the default result and returns the final result of the switch
// statement. It should be called at the end of the switch chain.
// The function is only called if no previous case has matched.
// Default 设置默认结果并返回 switch 语句的最终结果，应在链式调用末尾调用；
// 仅在没有分支命中时该结果才生效。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).Default("Other")	⏩ "Other"
func (s *switchBuilder[R, T]) Default(result R) R {
	if !s.matched {
		s.result = result
	}
	return s.result
}

// DefaultLazy is a variant of Default that accepts a lazy result function.
// It should be called at the end of the switch chain.
// The function is only called if no previous case has matched.
// DefaultLazy 是 Default 的变体，接受惰性结果函数，应在链式调用末尾调用；
// 仅在没有分支命中时才调用该函数。
//
// 🚀 EXAMPLE:
//
//	Switch[string](1).DefaultLazy(func() string{ return "Other" })	⏩ "Other"
func (s *switchBuilder[R, T]) DefaultLazy(resultF Lazy[R]) R {
	if !s.matched {
		s.result = resultF()
	}
	return s.result
}
