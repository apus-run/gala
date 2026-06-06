package rid_test

import (
	"fmt"
	"strings"

	"github.com/apus-run/gala/pkg/rid"
)

func ExampleResourceID_New() {
	const UserID rid.ResourceID = "user"

	resourceID := UserID.New(42)
	parts := strings.SplitN(resourceID, "-", 2)

	fmt.Println(parts[0])
	fmt.Println(len(parts[1]))

	// Output:
	// user
	// 6
}

func ExampleNewResourceID() {
	orderID := rid.NewResourceID("order")

	fmt.Println(orderID.String())
	fmt.Println(strings.HasPrefix(orderID.New(1001), "order-"))

	// Output:
	// order
	// true
}
