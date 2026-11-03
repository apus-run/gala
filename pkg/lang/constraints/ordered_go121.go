//go:build go1.21

package constraints

import (
	"cmp"
)

// Ordered is an alias of cmp.Ordered on Go 1.21+ | Go 1.21+ 下 Ordered 是 cmp.Ordered 的别名.
type Ordered = cmp.Ordered
