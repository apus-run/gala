package result

import (
	"fmt"
	"io"
	"strconv"
)

func Example() {
	fmt.Println(Of(strconv.Atoi("1")).Value())        // 1
	fmt.Println(Err[int](io.EOF).IsErr())             // true
	fmt.Println(Err[int](io.EOF).ValueOr(10))         // 10
	fmt.Println(OK(1).IsOK())                         // true
	fmt.Println(OK(1).ValueOrZero())                  // 1
	fmt.Println(Of(strconv.Atoi("x")).Option().Get()) // 0 false
	fmt.Println(Map(OK(1), strconv.Itoa).Get())       // "1" nil

	// Output:
	// 1
	// true
	// 10
	// true
	// 1
	// 0 false
	// 1 <nil>
}
