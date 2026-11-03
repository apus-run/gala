package value

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

// AnyValue wraps a value of any type together with an optional error so it can
// be converted to concrete types | AnyValue 包装任意类型的值与可选的错误，便于按具体类型取值.
type AnyValue struct {
	// Value holds the wrapped value | 被包装的值.
	Value any
	// Error holds an error that short-circuits every accessor | 持有错误时所有取值方法直接返回该错误.
	Error error
}

// Int returns the value as an int via strict type assertion | Int 以严格类型断言返回 int 值.
func (av AnyValue) Int() (int, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(int)
	if !ok {
		return 0, NewErrInvalidType("int", av.Value)
	}
	return val, nil
}

// AsInt returns the value as an int, parsing strings when needed | AsInt 返回 int，必要时解析字符串.
func (av AnyValue) AsInt() (int, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case int:
		return v, nil
	case string:
		res, err := strconv.ParseInt(v, 10, 64)
		return int(res), err
	}
	return 0, NewErrInvalidType("int", av.Value)
}

// IntOrDefault returns the value as an int, or the default on error | IntOrDefault 返回 int，出错时返回默认值.
func (av AnyValue) IntOrDefault(def int) int {
	val, err := av.Int()
	if err != nil {
		return def
	}
	return val
}

// Uint returns the value as a uint via strict type assertion | Uint 以严格类型断言返回 uint 值.
func (av AnyValue) Uint() (uint, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(uint)
	if !ok {
		return 0, NewErrInvalidType("uint", av.Value)
	}
	return val, nil
}

// AsUint returns the value as a uint, parsing strings when needed | AsUint 返回 uint，必要时解析字符串.
func (av AnyValue) AsUint() (uint, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case uint:
		return v, nil
	case string:
		res, err := strconv.ParseUint(v, 10, 64)
		return uint(res), err
	}
	return 0, NewErrInvalidType("uint", av.Value)
}

// UintOrDefault returns the value as a uint, or the default on error | UintOrDefault 返回 uint，出错时返回默认值.
func (av AnyValue) UintOrDefault(def uint) uint {
	val, err := av.Uint()
	if err != nil {
		return def
	}
	return val
}

// Int8 returns the value as an int8 via strict type assertion | Int8 以严格类型断言返回 int8 值.
func (av AnyValue) Int8() (int8, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(int8)
	if !ok {
		return 0, NewErrInvalidType("int", av.Value)
	}
	return val, nil
}

// AsInt8 returns the value as an int8, parsing strings when needed | AsInt8 返回 int8，必要时解析字符串.
func (av AnyValue) AsInt8() (int8, error) {
	if av.Error != nil {
		return 0, av.Error
	}

	switch v := av.Value.(type) {
	case int8:
		return v, nil
	case string:
		res, err := strconv.ParseInt(v, 10, 64)
		return int8(res), err
	}
	return 0, NewErrInvalidType("int8", av.Value)
}

// Int8OrDefault returns the value as an int8, or the default on error | Int8OrDefault 返回 int8，出错时返回默认值.
func (av AnyValue) Int8OrDefault(def int8) int8 {
	val, err := av.Int8()
	if err != nil {
		return def
	}
	return val
}

// Uint8 returns the value as a uint8 via strict type assertion | Uint8 以严格类型断言返回 uint8 值.
func (av AnyValue) Uint8() (uint8, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(uint8)
	if !ok {
		return 0, NewErrInvalidType("uint8", av.Value)
	}
	return val, nil
}

// AsUint8 returns the value as a uint8, parsing strings when needed | AsUint8 返回 uint8，必要时解析字符串.
func (av AnyValue) AsUint8() (uint8, error) {
	if av.Error != nil {
		return 0, av.Error
	}

	switch v := av.Value.(type) {
	case uint8:
		return v, nil
	case string:
		res, err := strconv.ParseUint(v, 10, 8)
		return uint8(res), err
	}
	return 0, NewErrInvalidType("uint8", av.Value)
}

// Uint8OrDefault returns the value as a uint8, or the default on error | Uint8OrDefault 返回 uint8，出错时返回默认值.
func (av AnyValue) Uint8OrDefault(def uint8) uint8 {
	val, err := av.Uint8()
	if err != nil {
		return def
	}
	return val
}

// Int16 returns the value as an int16 via strict type assertion | Int16 以严格类型断言返回 int16 值.
func (av AnyValue) Int16() (int16, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(int16)
	if !ok {
		return 0, NewErrInvalidType("int16", av.Value)
	}
	return val, nil
}

// AsInt16 returns the value as an int16, parsing strings when needed | AsInt16 返回 int16，必要时解析字符串.
func (av AnyValue) AsInt16() (int16, error) {
	if av.Error != nil {
		return 0, av.Error
	}

	switch v := av.Value.(type) {
	case int16:
		return v, nil
	case string:
		res, err := strconv.ParseInt(v, 10, 16)
		return int16(res), err
	}
	return 0, NewErrInvalidType("int16", av.Value)
}

// Int16OrDefault returns the value as an int16, or the default on error | Int16OrDefault 返回 int16，出错时返回默认值.
func (av AnyValue) Int16OrDefault(def int16) int16 {
	val, err := av.Int16()
	if err != nil {
		return def
	}
	return val
}

// Uint16 returns the value as a uint16 via strict type assertion | Uint16 以严格类型断言返回 uint16 值.
func (av AnyValue) Uint16() (uint16, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(uint16)
	if !ok {
		return 0, NewErrInvalidType("uint16", av.Value)
	}
	return val, nil
}

// AsUint16 returns the value as a uint16, parsing strings when needed | AsUint16 返回 uint16，必要时解析字符串.
func (av AnyValue) AsUint16() (uint16, error) {
	if av.Error != nil {
		return 0, av.Error
	}

	switch v := av.Value.(type) {
	case uint16:
		return v, nil
	case string:
		res, err := strconv.ParseUint(v, 10, 16)
		return uint16(res), err
	}
	return 0, NewErrInvalidType("uint16", av.Value)
}

// Uint16OrDefault returns the value as a uint16, or the default on error | Uint16OrDefault 返回 uint16，出错时返回默认值.
func (av AnyValue) Uint16OrDefault(def uint16) uint16 {
	val, err := av.Uint16()
	if err != nil {
		return def
	}
	return val
}

// Int32 returns the value as an int32 via strict type assertion | Int32 以严格类型断言返回 int32 值.
func (av AnyValue) Int32() (int32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(int32)
	if !ok {
		return 0, NewErrInvalidType("int32", av.Value)
	}
	return val, nil
}

// AsInt32 returns the value as an int32, parsing strings when needed | AsInt32 返回 int32，必要时解析字符串.
func (av AnyValue) AsInt32() (int32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case int32:
		return v, nil
	case string:
		res, err := strconv.ParseInt(v, 10, 32)
		return int32(res), err
	}
	return 0, NewErrInvalidType("int32", av.Value)
}

// Int32OrDefault returns the value as an int32, or the default on error | Int32OrDefault 返回 int32，出错时返回默认值.
func (av AnyValue) Int32OrDefault(def int32) int32 {
	val, err := av.Int32()
	if err != nil {
		return def
	}
	return val
}

// Uint32 returns the value as a uint32 via strict type assertion | Uint32 以严格类型断言返回 uint32 值.
func (av AnyValue) Uint32() (uint32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(uint32)
	if !ok {
		return 0, NewErrInvalidType("uint32", av.Value)
	}
	return val, nil
}

// AsUint32 returns the value as a uint32, parsing strings when needed | AsUint32 返回 uint32，必要时解析字符串.
func (av AnyValue) AsUint32() (uint32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case uint32:
		return v, nil
	case string:
		res, err := strconv.ParseUint(v, 10, 32)
		return uint32(res), err
	}
	return 0, NewErrInvalidType("uint32", av.Value)
}

// Uint32OrDefault returns the value as a uint32, or the default on error | Uint32OrDefault 返回 uint32，出错时返回默认值.
func (av AnyValue) Uint32OrDefault(def uint32) uint32 {
	val, err := av.Uint32()
	if err != nil {
		return def
	}
	return val
}

// Int64 returns the value as an int64 via strict type assertion | Int64 以严格类型断言返回 int64 值.
func (av AnyValue) Int64() (int64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(int64)
	if !ok {
		return 0, NewErrInvalidType("int64", av.Value)
	}
	return val, nil
}

// AsInt64 returns the value as an int64, parsing strings when needed | AsInt64 返回 int64，必要时解析字符串.
func (av AnyValue) AsInt64() (int64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	}
	return 0, NewErrInvalidType("int64", av.Value)
}

// Int64OrDefault returns the value as an int64, or the default on error | Int64OrDefault 返回 int64，出错时返回默认值.
func (av AnyValue) Int64OrDefault(def int64) int64 {
	val, err := av.Int64()
	if err != nil {
		return def
	}
	return val
}

// Uint64 returns the value as a uint64 via strict type assertion | Uint64 以严格类型断言返回 uint64 值.
func (av AnyValue) Uint64() (uint64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(uint64)
	if !ok {
		return 0, NewErrInvalidType("uint64", av.Value)
	}
	return val, nil
}

// AsUint64 returns the value as a uint64, parsing strings when needed | AsUint64 返回 uint64，必要时解析字符串.
func (av AnyValue) AsUint64() (uint64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case uint64:
		return v, nil
	case string:
		return strconv.ParseUint(v, 10, 64)
	}
	return 0, NewErrInvalidType("uint64", av.Value)
}

// Uint64OrDefault returns the value as a uint64, or the default on error | Uint64OrDefault 返回 uint64，出错时返回默认值.
func (av AnyValue) Uint64OrDefault(def uint64) uint64 {
	val, err := av.Uint64()
	if err != nil {
		return def
	}
	return val
}

// Float32 returns the value as a float32 via strict type assertion | Float32 以严格类型断言返回 float32 值.
func (av AnyValue) Float32() (float32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(float32)
	if !ok {
		return 0, NewErrInvalidType("float32", av.Value)
	}
	return val, nil
}

// AsFloat32 returns the value as a float32, parsing strings when needed | AsFloat32 返回 float32，必要时解析字符串.
func (av AnyValue) AsFloat32() (float32, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case float32:
		return v, nil
	case string:
		res, err := strconv.ParseFloat(v, 32)
		return float32(res), err
	}
	return 0, NewErrInvalidType("float32", av.Value)
}

// Float32OrDefault returns the value as a float32, or the default on error | Float32OrDefault 返回 float32，出错时返回默认值.
func (av AnyValue) Float32OrDefault(def float32) float32 {
	val, err := av.Float32()
	if err != nil {
		return def
	}
	return val
}

// Float64 returns the value as a float64 via strict type assertion | Float64 以严格类型断言返回 float64 值.
func (av AnyValue) Float64() (float64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	val, ok := av.Value.(float64)
	if !ok {
		return 0, NewErrInvalidType("float64", av.Value)
	}
	return val, nil
}

// AsFloat64 returns the value as a float64, parsing strings when needed | AsFloat64 返回 float64，必要时解析字符串.
func (av AnyValue) AsFloat64() (float64, error) {
	if av.Error != nil {
		return 0, av.Error
	}
	switch v := av.Value.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	}
	return 0, NewErrInvalidType("float64", av.Value)
}

// Float64OrDefault returns the value as a float64, or the default on error | Float64OrDefault 返回 float64，出错时返回默认值.
func (av AnyValue) Float64OrDefault(def float64) float64 {
	val, err := av.Float64()
	if err != nil {
		return def
	}
	return val
}

// String returns the value as a string via strict type assertion | String 以严格类型断言返回 string 值.
func (av AnyValue) String() (string, error) {
	if av.Error != nil {
		return "", av.Error
	}
	val, ok := av.Value.(string)
	if !ok {
		return "", NewErrInvalidType("string", av.Value)
	}
	return val, nil
}

// AsString converts the value to a string, formatting numbers and byte slices
// when needed | AsString 转为字符串，必要时格式化数值与字节切片.
func (av AnyValue) AsString() (string, error) {
	if av.Error != nil {
		return "", av.Error
	}

	var val string
	valueOf := reflect.ValueOf(av.Value)
	switch valueOf.Type().Kind() {
	case reflect.String:
		val = valueOf.String()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val = strconv.FormatUint(valueOf.Uint(), 10)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = strconv.FormatInt(valueOf.Int(), 10)
	case reflect.Float32:
		val = strconv.FormatFloat(valueOf.Float(), 'f', 10, 32)
	case reflect.Float64:
		val = strconv.FormatFloat(valueOf.Float(), 'f', 10, 64)
	case reflect.Slice:
		if valueOf.Type().Elem().Kind() != reflect.Uint8 {
			return "", NewErrInvalidType("[]byte", av.Value)
		}
		val = string(valueOf.Bytes())
	default:
		return "", errors.New("未兼容类型，暂时无法转换")
	}

	return val, nil
}

// StringOrDefault returns the value as a string, or the default on error | StringOrDefault 返回 string，出错时返回默认值.
func (av AnyValue) StringOrDefault(def string) string {
	val, err := av.String()
	if err != nil {
		return def
	}
	return val
}

// Bytes returns the value as a []byte via strict type assertion | Bytes 以严格类型断言返回 []byte 值.
func (av AnyValue) Bytes() ([]byte, error) {
	if av.Error != nil {
		return nil, av.Error
	}
	val, ok := av.Value.([]byte)
	if !ok {
		return nil, NewErrInvalidType("[]byte", av.Value)
	}
	return val, nil
}

// AsBytes returns the value as a []byte, converting strings when needed | AsBytes 返回 []byte，必要时转换字符串.
func (av AnyValue) AsBytes() ([]byte, error) {
	if av.Error != nil {
		return []byte{}, av.Error
	}
	switch v := av.Value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	}

	return []byte{}, NewErrInvalidType("[]byte", av.Value)
}

// BytesOrDefault returns the value as a []byte, or the default on error | BytesOrDefault 返回 []byte，出错时返回默认值.
func (av AnyValue) BytesOrDefault(def []byte) []byte {
	val, err := av.Bytes()
	if err != nil {
		return def
	}
	return val
}

// Bool returns the value as a bool via strict type assertion | Bool 以严格类型断言返回 bool 值.
func (av AnyValue) Bool() (bool, error) {
	if av.Error != nil {
		return false, av.Error
	}
	val, ok := av.Value.(bool)
	if !ok {
		return false, NewErrInvalidType("bool", av.Value)
	}
	return val, nil
}

// BoolOrDefault returns the value as a bool, or the default on error | BoolOrDefault 返回 bool，出错时返回默认值.
func (av AnyValue) BoolOrDefault(def bool) bool {
	val, err := av.Bool()
	if err != nil {
		return def
	}
	return val
}

// JSONScan unmarshals the value into the given object | JSONScan 将值反序列化到给定对象.
func (av AnyValue) JSONScan(val any) error {
	data, err := av.AsBytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, val)
}

// NewErrInvalidType creates an error representing a failed type conversion | NewErrInvalidType 创建一个代表类型转换失败的错误.
func NewErrInvalidType(want string, got any) error {
	return fmt.Errorf("value: 类型转换失败，预期类型:%s, 实际值:%#v", want, got)
}
