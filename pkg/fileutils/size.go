package fileutils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var sizeSuffixes = [...]string{"B", "K", "M", "G", "T", "P", "E"}

// FormatSize formats a byte count for humans using binary (1024) units,
// e.g. 1536 becomes "1.50K".
// FormatSize 按二进制（1024）单位把字节数格式化成人类可读的形式，
// 例如 1536 变成 "1.50K"。
func FormatSize(n int64) string {
	f, i := float64(n), 0
	for f >= 1024 && i < len(sizeSuffixes)-1 {
		f /= 1024
		i++
	}
	return fmt.Sprintf("%.2f%s", f, sizeSuffixes[i])
}

// unitShift is the number of bits between consecutive binary units.
// unitShift 是相邻二进制单位之间的位数.
const unitShift = 10

const (
	kib = 1 << (unitShift * (iota + 1))
	mib
	gib
	tib
	pib
	eib
)

// sizeUnits maps a lower-case unit to its size in bytes. All units are binary.
// sizeUnits 把小写单位映射到字节大小，所有单位都是二进制。
var sizeUnits = map[string]float64{
	"": 1, "b": 1, "byte": 1, "bytes": 1,
	"k": kib, "kb": kib, "ki": kib, "kib": kib, "kilobyte": kib,
	"m": mib, "mb": mib, "mi": mib, "mib": mib, "mebibyte": mib,
	"g": gib, "gb": gib, "gi": gib, "gib": gib, "gigabyte": gib,
	"t": tib, "tb": tib, "ti": tib, "tib": tib, "terabyte": tib,
	"p": pib, "pb": pib, "pi": pib, "pib": pib, "petabyte": pib,
	"e": eib, "eb": eib, "ei": eib, "eib": eib, "exabyte": eib,
}

// ParseSize converts a size such as "10", "1.5K", "64 MB" or "2GiB" to bytes.
// Units are case-insensitive and binary (1K = 1024). It returns an error for
// a malformed number, an unknown unit, or a value that overflows int64.
// ParseSize 把 "10"、"1.5K"、"64 MB"、"2GiB" 这类大小转换为字节。
// 单位不区分大小写，按二进制（1K = 1024）。数字非法、单位未知或
// 溢出 int64 时返回错误。
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return r != '.' && (r < '0' || r > '9') })
	if i < 0 {
		i = len(s)
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("fileutils: invalid size %q", s)
	}
	mult, ok := sizeUnits[strings.ToLower(strings.TrimSpace(s[i:]))]
	if !ok {
		return 0, fmt.Errorf("fileutils: unknown size unit in %q", s)
	}
	if v := num * mult; v < math.MaxInt64 {
		return int64(v), nil
	}
	return 0, fmt.Errorf("fileutils: size %q overflows int64", s)
}
