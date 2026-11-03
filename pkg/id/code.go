package id

// NewCode can get a unique code by id (You need to ensure that id is unique).
// NewCode 按 id 生成一个短码（需自行保证 id 唯一）。
//
// Deprecated: the output is not injective (with N1=0 every input maps to the
// same code, and inputs past len(chars)^length collide), and out-of-range
// alphabets or lengths yield an empty string. Use NewCodeChecked in new code.
// Deprecated: 输出不是单射的（N1=0 时所有输入得到同一个码，输入超出
// len(chars)^length 后会碰撞），且字符表或长度超出 [1, 255] 时返回空串。
// 新代码请用 NewCodeChecked。
func NewCode(id uint64, options ...func(*CodeOptions)) string {
	ops := getCodeOptionsOrSetDefault(nil)
	for _, f := range options {
		f(ops)
	}
	// The legacy implementation does the modulo in a byte: with an alphabet
	// longer than 255 characters or a length above 255, byte(charLen)
	// truncates to 0 and the modulo below panics with a division by zero.
	// This only guards the crash; output for valid options is unchanged
	// (codes already issued must stay stable). For a collision-free encoding
	// with validated options use NewCodeChecked.
	// 旧实现用 byte 做取模：字符表超过 255 个字符、或长度超过 255 时
	// byte(charLen) 会截断成 0，下面的取模就是除零 panic。这里只防崩溃，
	// 不改变正常参数下的输出（已经发出去的码不能变）。需要无碰撞、范围可
	// 校验的编码请用 NewCodeChecked。
	if len(ops.chars) == 0 || len(ops.chars) > 255 || ops.l <= 0 || ops.l > 255 {
		return ""
	}
	// enlarge and add salt
	// 放大并加盐
	id = id*uint64(ops.n1) + ops.salt

	var code []rune
	slIdx := make([]byte, ops.l)

	charLen := len(ops.chars)
	charLenUI := uint64(charLen)

	// diffusion
	// 扩散
	for i := range ops.l {
		// get each number
		// 取出每一位数字
		slIdx[i] = byte(id % charLenUI)
		// let units digit affect other digit
		// 让个位影响其他位
		slIdx[i] = (slIdx[i] + byte(i)*slIdx[0]) % byte(charLen)
		// right shift
		// 右移
		id /= charLenUI
	}

	// confusion (https://en.wikipedia.org/wiki/Permutation_box)
	// 混淆（置换盒 https://en.wikipedia.org/wiki/Permutation_box）
	for i := range ops.l {
		idx := (byte(i) * byte(ops.n2)) % byte(ops.l)
		code = append(code, ops.chars[slIdx[idx]])
	}
	return string(code)
}
