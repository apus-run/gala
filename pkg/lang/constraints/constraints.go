// Package constraints defines a set of useful constraints to be used
// with type parameters.
// 包 constraints 定义一组可用于类型参数的常用约束。
package constraints

// Signed is a constraint that permits any signed integer type.
// If future releases of Go add new predeclared signed integer types,
// this constraint will be modified to include them.
// Signed 约束允许任意有符号整数类型；
// 若未来 Go 新增预声明有符号整数类型，本约束会随之扩充。
type Signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

// Unsigned is a constraint that permits any unsigned integer type.
// If future releases of Go add new predeclared unsigned integer types,
// this constraint will be modified to include them.
// Unsigned 约束允许任意无符号整数类型；
// 若未来 Go 新增预声明无符号整数类型，本约束会随之扩充。
type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Integer is a constraint that permits any integer type.
// If future releases of Go add new predeclared integer types,
// this constraint will be modified to include them.
// Integer 约束允许任意整数类型；
// 若未来 Go 新增预声明整数类型，本约束会随之扩充。
type Integer interface {
	Signed | Unsigned
}

// Float is a constraint that permits any floating-point type.
// If future releases of Go add new predeclared floating-point types,
// this constraint will be modified to include them.
// Float 约束允许任意浮点类型；
// 若未来 Go 新增预声明浮点类型，本约束会随之扩充。
type Float interface {
	~float32 | ~float64
}

// Complex is a constraint that permits any complex numeric type.
// If future releases of Go add new predeclared complex numeric types,
// this constraint will be modified to include them.
// Complex 约束允许任意复数类型；
// 若未来 Go 新增预声明复数类型，本约束会随之扩充。
type Complex interface {
	~complex64 | ~complex128
}

// Number is a constraint that permits any numeric type.
// Number 约束允许任意数值类型。
type Number interface {
	Integer | Float
}
