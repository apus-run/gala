//go:build !go1.21

package constraints

// Ordered is a constraint that permits any ordered type: any type
// that supports the operators < <= >= >.
// If future releases of Go add new ordered types,
// this constraint will be modified to include them.
// Ordered 约束允许任意有序类型，即支持 < <= >= > 运算符的类型；
// 若未来 Go 新增有序类型，本约束会随之扩充。
type Ordered interface {
	Number | ~string
}
