// Package mathutils provides small numeric helpers such as Euclidean modulus
// and decimal rounding.
// 包 mathutils 提供数值小工具，如欧几里得取模与按小数位四舍五入。
package mathutils

// Mod returns the Eucledian modulus of a and b.
// Mod 返回 a 与 b 的欧几里得模（结果恒非负）。
func Mod(a, b int) int {
	r := a % b
	if r < 0 {
		if b < 0 {
			r -= b
		} else {
			r += b
		}
	}
	return r
}
