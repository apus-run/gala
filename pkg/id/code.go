package id

// NewCode can get a unique code by id(You need to ensure that id is unique).
//
// Deprecated: 输出不是单射的（N1=0 时所有输入得到同一个码，输入超出 len(chars)^length 后会碰撞），
// 且字符表或长度超出 [1, 255] 时返回空串。新代码请用 NewCodeChecked。
func NewCode(id uint64, options ...func(*CodeOptions)) string {
	ops := getCodeOptionsOrSetDefault(nil)
	for _, f := range options {
		f(ops)
	}
	// 旧实现用 byte 做取模：字符表超过 255 个字符、或长度超过 255 时 byte(charLen) 会截断成 0，
	// 下面的取模就是除零 panic。这里只防崩溃，不改变正常参数下的输出（已经发出去的码不能变）。
	// 需要无碰撞、范围可校验的编码请用 NewCodeChecked。
	if len(ops.chars) == 0 || len(ops.chars) > 255 || ops.l <= 0 || ops.l > 255 {
		return ""
	}
	// enlarge and add salt
	id = id*uint64(ops.n1) + ops.salt

	var code []rune
	slIdx := make([]byte, ops.l)

	charLen := len(ops.chars)
	charLenUI := uint64(charLen)

	// diffusion
	for i := range ops.l {
		slIdx[i] = byte(id % charLenUI)                          // get each number
		slIdx[i] = (slIdx[i] + byte(i)*slIdx[0]) % byte(charLen) // let units digit affect other digit
		id /= charLenUI                                          // right shift
	}

	// confusion(https://en.wikipedia.org/wiki/Permutation_box)
	for i := range ops.l {
		idx := (byte(i) * byte(ops.n2)) % byte(ops.l)
		code = append(code, ops.chars[slIdx[idx]])
	}
	return string(code)
}
