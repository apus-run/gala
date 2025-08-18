package cond

import (
	"fmt"
)

func Example() {
	fmt.Println(If(true, 1, 2)) // 1
	var a *struct{ A int }
	getA := func() int { return a.A }
	get1 := func() int { return 1 }
	fmt.Println(IfLazy(a != nil, getA, get1)) // 1
	fmt.Println(IfLazyL(a != nil, getA, 1))   // 1
	fmt.Println(IfLazyR(a == nil, 1, getA))   // 1

	fmt.Println(Switch[string](3).
		Case(1, "1").
		CaseLazy(2, func() string { return "3" }).
		When(3, 4).Then("3/4").
		When(5, 6).ThenLazy(func() string { return "5/6" }).
		Default("other")) // 3/4

	// Output:
	// 1
	// 1
	// 1
	// 1
	// 3/4
}
