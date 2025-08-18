package conv

import (
	"fmt"

	"github.com/apus-run/gala/pkg/lang/ptr"
)

func Example() {
	fmt.Println(To[string](1))                        // "1"
	fmt.Println(To[int]("1"))                         // 1
	fmt.Println(To[int]("x"))                         // 0
	fmt.Println(To[bool]("true"))                     // true
	fmt.Println(To[bool]("x"))                        // false
	fmt.Println(To[int](ptr.Of(ptr.Of(ptr.Of("1"))))) // 1
	type myInt int
	type myString string
	fmt.Println(To[myInt](myString("1"))) // 1
	fmt.Println(To[myString](myInt(1)))   // "1"

	fmt.Println(ToE[int]("x"))      // 0 strconv.ParseInt: parsing "x": invalid syntax
	fmt.Println(ToE[int]("1.10"))   // 0 strconv.ParseInt: parsing "1.1": invalid syntax
	fmt.Println(ToE[int]("1.00"))   // 1 nil
	fmt.Println(ToE[float64](".1")) // 0.1 nil

	// Output:
	// 1
	// 1
	// 0
	// true
	// false
	// 1
	// 1
	// 1
	// 0 strconv.ParseInt: parsing "x": invalid syntax
	// 0 strconv.ParseInt: parsing "1.1": invalid syntax
	// 1 <nil>
	// 0.1 <nil>
}
