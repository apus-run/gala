package of

import (
	"fmt"
	"strconv"
)

func Example() {
	fmt.Println(Of(1, true).Value())            // 1
	fmt.Println(Nil[int]().IsNil())             // true
	fmt.Println(Nil[int]().ValueOr(10))         // 10
	fmt.Println(OK(1).IsOK())                   // true
	fmt.Println(OK(1).ValueOrZero())            // 1
	fmt.Println(OfPtr((*int)(nil)).Ptr())       // nil
	fmt.Println(Map(OK(1), strconv.Itoa).Get()) // "1" true

	// Output:
	// 1
	// true
	// 10
	// true
	// 1
	// <nil>
	// 1 true
}
