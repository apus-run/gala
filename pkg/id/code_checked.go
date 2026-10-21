package id

import (
	"fmt"
	"math/big"
	"unicode/utf8"
)

// NewCodeChecked encodes an ID injectively within [0, len(chars)^length).
// It validates the alphabet and permutations and uses exact arithmetic.
// It is not encryption. Legacy NewCode remains available for compatibility.
func NewCodeChecked(value uint64, options ...func(*CodeOptions)) (string, error) {
	o := getCodeOptionsOrSetDefault(nil)
	o.strict = true
	for _, option := range options {
		option(o)
	}
	base := len(o.chars)
	if base < 2 || o.l < 1 || o.l > 255 || o.n1 <= 0 || o.n2 <= 0 {
		return "", fmt.Errorf("id: invalid code alphabet, length or multipliers")
	}
	seen := make(map[rune]bool, base)
	for _, ch := range o.chars {
		if !utf8.ValidRune(ch) {
			return "", fmt.Errorf("id: invalid Unicode code character")
		}
		if seen[ch] {
			return "", fmt.Errorf("id: duplicate code character %q", ch)
		}
		seen[ch] = true
	}
	gcd := func(a, b int) int {
		for b != 0 {
			a, b = b, a%b
		}
		return a
	}
	if gcd(o.n1, base) != 1 || gcd(o.n2, o.l) != 1 {
		return "", fmt.Errorf("id: code multipliers must be coprime to alphabet size and length")
	}
	radix := big.NewInt(int64(base))
	capacity := new(big.Int).Exp(radix, big.NewInt(int64(o.l)), nil)
	n := new(big.Int).SetUint64(value)
	if n.Cmp(capacity) >= 0 {
		return "", fmt.Errorf("id: input exceeds code capacity")
	}
	n.Mul(n, big.NewInt(int64(o.n1)))
	n.Add(n, new(big.Int).SetUint64(o.salt))
	digits := make([]int, o.l)
	remainder := new(big.Int)
	for i := range digits {
		n.QuoRem(n, radix, remainder)
		digits[i] = (int(remainder.Int64()) + i*digits[0]) % base
	}
	result := make([]rune, o.l)
	for i := range result {
		result[i] = o.chars[digits[(i*(o.n2%o.l))%o.l]]
	}
	return string(result), nil
}
