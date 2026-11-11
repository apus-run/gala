package conf

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	errInvalidType  = errors.New("invalid type for binding")
	errUnknownKey   = errors.New("unknown key")
	durationType    = reflect.TypeFor[time.Duration]()
	errStringType   = errors.New("string required; quote string values")
	errTypeMismatch = errors.New("type mismatch")
	errRange        = errors.New("value out of range")
	errNotInteger   = errors.New("not an exact integer")
	errNotUnit      = errors.New("duration needs an explicit unit")
	decimalText     = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
	jsonNumberText  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
)

// A frozen path keeps the last safe prefix below collections and unsafe tags.
type fieldPath struct {
	text   string
	frozen bool
}

func (p fieldPath) freeze() fieldPath {
	p.frozen = true
	return p
}

func (p fieldPath) child(name string) fieldPath {
	if p.frozen {
		return p
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return p.freeze()
		}
	}
	if p.text == "" {
		p.text = name
	} else {
		p.text += "." + name
	}
	return p
}

func fieldName(f reflect.StructField) string {
	name := f.Tag.Get("conf")
	if !f.IsExported() || name == "-" {
		return ""
	}
	if name == "" {
		return f.Name
	}
	return name
}

func validateTarget(t reflect.Type) error {
	if t.Kind() != reflect.Struct {
		return &Error{Stage: StageConfig, Source: -1, Err: errors.New("T must be a struct value type")}
	}
	return inspectType(t, fieldPath{}, make(map[reflect.Type]bool), false)
}

// Entries are false while visiting and true once every descendant is checked.
func inspectType(t reflect.Type, path fieldPath, seen map[reflect.Type]bool, allowAny bool) error {
	invalid := func(p fieldPath) error {
		return &Error{Stage: StageConfig, Source: -1, Path: p.text, Err: errInvalidType}
	}
	if done, visited := seen[t]; visited {
		if done {
			return nil
		}
		return invalid(path)
	}
	seen[t] = false
	switch t.Kind() {
	case reflect.Interface:
		if !allowAny || t.NumMethod() != 0 {
			return invalid(path)
		}
	case reflect.Struct:
		names := make(map[string]bool)
		for f := range t.Fields() {
			name := fieldName(f)
			if name == "" {
				continue
			}
			if strings.ContainsRune(name, ',') || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				return invalid(path.child(f.Name))
			}
			field := path.child(name)
			if f.Anonymous || names[name] {
				return invalid(field)
			}
			names[name] = true
			if err := inspectType(f.Type, field, seen, allowAny); err != nil {
				return err
			}
		}
		if len(names) == 0 {
			return invalid(path)
		}
	case reflect.Map, reflect.Slice, reflect.Pointer:
		if t.Kind() == reflect.Map && t.Key().Kind() != reflect.String {
			return invalid(path)
		}
		if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Pointer {
			return invalid(path)
		}
		if t.Kind() != reflect.Pointer {
			path = path.freeze()
		}
		if err := inspectType(t.Elem(), path, seen, allowAny); err != nil {
			return err
		}
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
	default:
		return invalid(path)
	}
	seen[t] = true
	return nil
}

// data has already been normalized by the merger; dst is a private candidate.
func bindValue(data any, dst reflect.Value, path fieldPath) error {
	bad := func(cause error) error {
		return &Error{Stage: StageBind, Source: -1, Path: path.text, Err: cause}
	}
	switch dst.Kind() {
	case reflect.Interface:
		dst.Set(reflect.ValueOf(copyTreeValue(data)))
	case reflect.Struct:
		object, ok := data.(map[string]any)
		if !ok {
			return bad(errTypeMismatch)
		}
		matched := 0
		for f := range dst.Type().Fields() {
			name := fieldName(f)
			if name == "" {
				continue
			}
			if value, exists := object[name]; exists {
				if err := bindValue(value, dst.FieldByIndex(f.Index), path.child(name)); err != nil {
					return err
				}
				matched++
			}
		}
		if matched != len(object) {
			return bad(errUnknownKey)
		}
	case reflect.Map:
		object, ok := data.(map[string]any)
		if !ok {
			return bad(errTypeMismatch)
		}
		out := reflect.MakeMapWithSize(dst.Type(), len(object))
		for key, value := range object {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := bindValue(value, elem, path.freeze()); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(key).Convert(dst.Type().Key()), elem)
		}
		dst.Set(out)
	case reflect.Slice:
		list, ok := data.([]any)
		if !ok {
			return bad(errTypeMismatch)
		}
		out := reflect.MakeSlice(dst.Type(), len(list), len(list))
		for i, value := range list {
			if err := bindValue(value, out.Index(i), path.freeze()); err != nil {
				return err
			}
		}
		dst.Set(out)
	case reflect.Pointer:
		ptr := reflect.New(dst.Type().Elem())
		if err := bindValue(data, ptr.Elem(), path); err != nil {
			return err
		}
		dst.Set(ptr)
	default:
		if err := setScalar(dst, data); err != nil {
			return bad(err)
		}
	}
	return nil
}

func setScalar(dst reflect.Value, data any) error {
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return errTypeMismatch
	}
	number, isNumber := data.(json.Number)
	if isNumber && !jsonNumberText.MatchString(string(number)) {
		return errTypeMismatch
	}
	if dst.Type() == durationType {
		if v.Kind() != reflect.String || isNumber {
			if isNumber || isNumericKind(v.Kind()) {
				return errNotUnit
			}
			return errTypeMismatch
		}
		if decimalText.MatchString(v.String()) {
			return errNotUnit
		}
		d, err := time.ParseDuration(v.String())
		if err != nil {
			return errTypeMismatch
		}
		dst.SetInt(int64(d))
		return nil
	}
	switch dst.Kind() {
	case reflect.String:
		if v.Kind() != reflect.String || isNumber {
			return errStringType
		}
		dst.SetString(v.String())
	case reflect.Bool:
		switch {
		case v.Kind() == reflect.Bool:
			dst.SetBool(v.Bool())
		case v.Kind() == reflect.String && !isNumber && (v.String() == "true" || v.String() == "false"):
			dst.SetBool(v.String() == "true")
		default:
			return errTypeMismatch
		}
	case reflect.Float32, reflect.Float64:
		return setFloat(dst, v)
	default:
		return setInteger(dst, v)
	}
	return nil
}

func isNumericKind(k reflect.Kind) bool {
	return (k >= reflect.Int && k <= reflect.Uint64) || k == reflect.Float32 || k == reflect.Float64
}

func setInteger(dst, v reflect.Value) error {
	var magnitude uint64
	var negative bool
	switch k := v.Kind(); {
	case k >= reflect.Int && k <= reflect.Int64:
		n := v.Int()
		negative = n < 0
		if negative {
			magnitude = uint64(-n)
		} else {
			magnitude = uint64(n)
		}
	case k >= reflect.Uint && k <= reflect.Uint64:
		magnitude = v.Uint()
	case k == reflect.Float32 || k == reflect.Float64:
		n := v.Float()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return errTypeMismatch
		}
		if math.Trunc(n) != n {
			return errNotInteger
		}
		limit := float64(1 << 53)
		if k == reflect.Float32 {
			limit = 1 << 24
		}
		if math.Abs(n) >= limit {
			return errRange
		}
		negative = n < 0
		magnitude = uint64(math.Abs(n))
	case k == reflect.String:
		s := v.String()
		if !decimalText.MatchString(s) {
			return errTypeMismatch
		}
		if strings.ContainsAny(s, ".eE") {
			return errNotInteger
		}
		negative = s[0] == '-'
		if negative || s[0] == '+' {
			s = s[1:]
		}
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return errRange
		}
		magnitude = n
	default:
		return errTypeMismatch
	}
	if dst.Kind() >= reflect.Uint && dst.Kind() <= reflect.Uint64 {
		if (negative && magnitude != 0) || dst.OverflowUint(magnitude) {
			return errRange
		}
		dst.SetUint(magnitude)
		return nil
	}
	bound := uint64(1) << (dst.Type().Bits() - 1)
	if magnitude > bound || (magnitude == bound && !negative) {
		return errRange
	}
	n := int64(magnitude)
	if negative {
		n = -n
	}
	dst.SetInt(n)
	return nil
}

func setFloat(dst, v reflect.Value) error {
	var f float64
	single := dst.Kind() == reflect.Float32
	switch k := v.Kind(); {
	case k >= reflect.Int && k <= reflect.Int64:
		if single {
			f = float64(float32(v.Int()))
		} else {
			f = float64(v.Int())
		}
	case k >= reflect.Uint && k <= reflect.Uint64:
		if single {
			f = float64(float32(v.Uint()))
		} else {
			f = float64(v.Uint())
		}
	case k == reflect.Float32 || k == reflect.Float64:
		f = v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return errTypeMismatch
		}
	case k == reflect.String:
		if !decimalText.MatchString(v.String()) {
			return errTypeMismatch
		}
		n, err := strconv.ParseFloat(v.String(), dst.Type().Bits())
		if err != nil {
			return errRange
		}
		f = n
	default:
		return errTypeMismatch
	}
	if single && math.IsInf(float64(float32(f)), 0) {
		return errRange
	}
	dst.SetFloat(f)
	return nil
}
