package conf

import (
	"errors"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-viper/mapstructure/v2"
)

var durationType = reflect.TypeFor[time.Duration]()

// bind decodes merged into out strictly. Unknown keys, exact-match only, and
// explicit type rules from hook.go. Errors carry the field path but never the
// raw value.
//
// bind 把 merged 严格解码进 out：未知键报错、精确匹配、类型规则见 hook。
// 错误只携带字段路径，不携带原始值。
func bind(merged map[string]any, out any) error {
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:      out,
		ErrorUnused: true,
		MatchName:   func(mapKey, fieldName string) bool { return mapKey == fieldName },
		DecodeHook:  mapstructure.ComposeDecodeHookFunc(strictHook),
	})
	if err != nil {
		return newError(StageBind, -1, "", "", err)
	}
	if err := dec.Decode(merged); err != nil {
		failure := newError(StageBind, -1, "", bindPath(err), nil)
		if errors.Is(err, errStringType) {
			failure.reason = "string required; quote string values"
		}
		return failure
	}
	return nil
}

// quotedName matches the leading field name mapstructure puts in each error.
// quotedName 匹配 mapstructure 每条错误开头的字段名。
var quotedName = regexp.MustCompile(`(?m)^(?:error decoding )?'([^']*)'(?: has invalid keys: ([^\n]+)| )`)

// bindPath returns the first field path from a mapstructure error, without
// any value text. It returns "" when no path can be recovered.
// bindPath 返回 mapstructure 错误中的第一个字段路径，不含任何值文本；
// 无法解析时返回 ""。
func bindPath(err error) string {
	return firstName(err.Error())
}

func firstName(msg string) string {
	if m := quotedName.FindStringSubmatch(msg); m != nil {
		if len(m) > 2 && m[2] != "" {
			key := strings.Split(m[2], ", ")[0]
			if m[1] == "" {
				return key
			}
			return m[1] + "." + key
		}
		return m[1]
	}
	return ""
}

// strictHook implements the conversion table in the spec. It returns data
// unchanged for targets and inputs it does not govern, so mapstructure's own
// rules still apply to them.
// strictHook 实现规格中的类型转换表。对不处理的目标与输入原样返回，
// 交给 mapstructure 的规则。
func strictHook(from reflect.Type, to reflect.Type, data any) (any, error) {
	if to == durationType {
		return parseDuration(data)
	}
	switch to.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return parseInt(to, data)
	case reflect.Bool:
		return parseBool(data)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Interface:
		return nil, errTypeMismatch
	case reflect.String:
		if _, ok := data.(string); !ok && data != nil {
			return nil, errStringType
		}
	}
	return data, nil
}

var (
	errStringType   = errors.New("string required; quote string values")
	errTypeMismatch = errors.New("type mismatch")
	errRange        = errors.New("value out of range")
	errNotInteger   = errors.New("not an exact integer")
	errNotUnit      = errors.New("duration needs an explicit unit")
)

func parseInt(to reflect.Type, data any) (any, error) {
	bits := to.Bits()
	lo := -(int64(1) << (bits - 1))
	hi := int64(1)<<(bits-1) - 1
	var n int64
	switch v := data.(type) {
	case string:
		p, err := strconv.ParseInt(v, 10, bits)
		if err != nil {
			return nil, errTypeMismatch
		}
		n = p
	default:
		value := reflect.ValueOf(data)
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n = value.Int()
		case reflect.Float32, reflect.Float64:
			v := value.Float()
			if v != math.Trunc(v) || math.IsInf(v, 0) || math.IsNaN(v) {
				return nil, errNotInteger
			}
			if v >= 1<<53 || v <= -(1<<53) {
				return nil, errRange
			}
			n = int64(v)
		default:
			return nil, errTypeMismatch
		}
	}

	if n < lo || n > hi {
		return nil, errRange
	}
	return n, nil
}

func parseBool(data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		if _, ok := data.(bool); ok {
			return data, nil
		}
		return nil, errTypeMismatch
	}
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return nil, errTypeMismatch
}

func parseDuration(data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		return nil, errTypeMismatch
	}
	if strings.IndexFunc(s, unicode.IsLetter) < 0 {
		return nil, errNotUnit
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, errTypeMismatch
	}
	return d, nil
}
