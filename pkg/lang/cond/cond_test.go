package cond

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIf(t *testing.T) {
	assert.Equal(t, 1, If(true, 1, 2))
	assert.Equal(t, 2, If(false, 1, 2))
	assert.Equal(t, "2", If(false, "1", "2"))
	assert.Equal(t, "1", If(true, "1", "2"))

	// assert.Equal(t, t.Name(), "")
	// assert.Equal(t, "", If(t == nil, "", t.Name()))
}

func lazy[T any](v T) Lazy[T] {
	return func() T {
		return v
	}
}

func TestIfLazy(t *testing.T) {
	assert.Equal(t, 1, IfLazy(true, lazy(1), lazy(2)))
	assert.Equal(t, 2, IfLazy(false, lazy(1), lazy(2)))
	assert.Equal(t, "1", IfLazy(true, lazy("1"), lazy("2")))
	assert.Equal(t, "2", IfLazy(false, lazy("1"), lazy("2")))

	// assert.Equal(t, "", IfLazy(t != nil, func() string { return t.Name() }, lazy("")))
	// assert.Equal(t, "", IfLazy(t == nil, lazy(""), func() string { return t.Name() }))
}

func TestIfLazyL(t *testing.T) {
	assert.Equal(t, 1, IfLazyL(true, lazy(1), 2))
	assert.Equal(t, 2, IfLazyL(false, lazy(1), 2))
	assert.Equal(t, "1", IfLazyL(true, lazy("1"), "2"))
	assert.Equal(t, "2", IfLazyL(false, lazy("1"), "2"))

	// assert.Equal(t, "", IfLazyL(t != nil, func() string { return t.Name() }, ""))
}

func TestIfLazyR(t *testing.T) {
	assert.Equal(t, 1, IfLazyR(true, 1, lazy(2)))
	assert.Equal(t, 2, IfLazyR(false, 1, lazy(2)))
	assert.Equal(t, "1", IfLazyR(true, "1", lazy("2")))
	assert.Equal(t, "2", IfLazyR(false, "1", lazy("2")))

	// assert.Equal(t, "", IfLazyR(t == nil, "", func() string { return t.Name() }))
}

func TestSwitch(t *testing.T) {
	v1 := Switch[string](1).
		Case(1, "1").
		Case(2, "2").
		CaseLazy(3, func() string { return "3" }).
		CaseLazy(4, func() string { return "4" }).
		Default("5")
	assert.Equal(t, v1, "1")

	v2 := Switch[string](3).
		Case(1, "1").
		Case(2, "2").
		CaseLazy(3, func() string { return "3" }).
		CaseLazy(4, func() string { return "4" }).
		Default("5")
	assert.Equal(t, v2, "3")

	v3 := Switch[string](10).
		Case(1, "1").
		Case(2, "2").
		CaseLazy(3, func() string { return "3" }).
		CaseLazy(4, func() string { return "4" }).
		Default("5")
	assert.Equal(t, v3, "5")
}

func TestSwitchWhen(t *testing.T) {
	v1 := Switch[string](1).
		When(1, 2).Then("1").
		When(3, 4).ThenLazy(func() string { return "3" }).
		DefaultLazy(func() string {
			return "5"
		})
	assert.Equal(t, v1, "1")

	v2 := Switch[string](4).
		When(1, 2).Then("1").
		When(3, 4).ThenLazy(func() string { return "3" }).
		DefaultLazy(func() string {
			return "5"
		})
	assert.Equal(t, v2, "3")

	v3 := Switch[string](10).
		When(1, 2).Then("1").
		When(3, 4).ThenLazy(func() string { return "3" }).
		DefaultLazy(func() string {
			return "5"
		})
	assert.Equal(t, v3, "5")
}
