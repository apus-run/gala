package of

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/guregu/null"
	jsonIter "github.com/json-iterator/go"

	"github.com/apus-run/gala/pkg/lang/value"
)

// O nullable type with support for various interfaces:
// json.Unmarshaler, json.Marshaler, encoding.TextUnmarshaler, encoding.TextMarshaler, sql.Scanner, driver.Valuer
type O[T any] struct {
	V     T
	Valid bool
}

func New[T any](v T, valid bool) O[T] {
	return O[T]{
		V:     v,
		Valid: valid,
	}
}

// Of creates an optional value with type T from tuple (T, bool).
//
// Of is used to wrap result of "func () (T, bool)".
//
// 💡 NOTE: If the given bool is false, the value of T MUST be zero value of T,
// Otherwise this will be an undefined behavior.
func Of[T any](v T, valid bool) O[T] {
	return O[T]{V: v, Valid: valid}
}

// OK creates an optional value O containing value v.
func OK[T any](v T) O[T] {
	return Of(v, true)
}

// Nil creates an optional value O containing nothing.
func Nil[T any]() O[T] {
	return O[T]{}
}

// OfPtr is a variant of function [Of], creates an optional value from pointer v.
//
// If v != nil, returns value that the pointer points to, else returns nothing.
func OfPtr[T any](v *T) O[T] {
	if v == nil {
		return Nil[T]()
	}
	return OK(*v)
}

func From[T any](v T) O[T] {
	return O[T]{
		V:     v,
		Valid: true,
	}
}

// Val returns internal value of O.
func (o O[T]) Val() T {
	return o.V
}

// ValueOr returns internal value of O.
// Custom value v is returned when O contains nothing.
func (o O[T]) ValueOr(v T) T {
	if o.Valid {
		return o.V
	}
	return v
}

// ValueOrZero returns the value if valid, otherwise returns the zero value of type T.
func (o O[T]) ValueOrZero() T {
	if o.Valid {
		return o.V
	}

	return value.Zero[T]()
}

// Ptr returns a pointer that points to the internal value of optional value O[T].
// Nil is returned when it contains nothing.
//
// 💡 NOTE: DON'T modify the internal value through the pointer,
// it won't work as you expect because the optional value is proposed to use as value,
// when you call method on it, it is copied.
func (o O[T]) Ptr() *T {
	if !o.Valid {
		return nil
	}
	return &o.V
}

// Get returns the optional value in (value, ok) form.
func (o O[T]) Get() (T, bool) {
	return o.V, o.Valid
}

// IsOK returns true when O contains value, otherwise false.
func (o O[T]) IsOK() bool {
	return o.Valid
}

// IsNil returns true when O contains nothing, otherwise false.
func (o O[T]) IsNil() bool {
	return !o.Valid
}

// IfOK calls function f when O contains value, otherwise do nothing.
func (o O[T]) IfOK(f func(T)) {
	if o.Valid {
		f(o.V)
	}
}

// IfNil calls function f when O contains nil, otherwise do nothing.
func (o O[T]) IfNil(f func()) {
	if !o.Valid {
		f()
	}
}

// Map applies function f to value of optional value O[F] if it contains value.
// Otherwise, Nil[T]() is returned.
func Map[F, T any](o O[F], f func(F) T) O[T] {
	if !o.Valid {
		return Nil[T]()
	}
	return OK(f(o.V))
}

// Then calls function f and returns its result if O[F] contains value.
// Otherwise, Nil[T]() is returned.
//
// 💡 HINT: This function is similar to the Rust's std::option::Option.and_then
func Then[F, T any](o O[F], f func(F) O[T]) O[T] {
	if !o.Valid {
		return Nil[T]()
	}
	return f(o.V)
}

// Type returns the string representation of type of optional value.
func (o O[T]) Type() string {
	ty := reflect.TypeOf(value.Zero[T]())
	if ty == nil {
		return "any"
	}
	return ty.String()
}

// String implements [fmt.Stringer].
func (o O[T]) String() string {
	if !o.Valid {
		return fmt.Sprintf("goption.Nil[%s]()", o.Type())
	}
	return fmt.Sprintf("goption.OK[%s](%v)", o.Type(), o.V)
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (o *O[T]) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		var t T
		o.V, o.Valid = t, false
		return nil
	}

	if u, ok := any(&o.V).(json.Unmarshaler); ok {
		if err := u.UnmarshalJSON(data); err != nil {
			return err
		}
		o.Valid = true
		return nil
	}
	if err := jsonIter.ConfigFastest.Unmarshal(data, &o.V); err != nil {
		return err
	}
	o.Valid = true
	return nil
}

// MarshalJSON implements json.Marshaler interface.
func (o O[T]) MarshalJSON() ([]byte, error) {
	if !o.Valid {
		return jsonIter.ConfigFastest.Marshal(nil)
	}
	if m, ok := any(o.V).(json.Marshaler); ok {
		return m.MarshalJSON()
	}
	return jsonIter.ConfigFastest.Marshal(o.V)
}

// UnmarshalText implements encoding.TextUnmarshaler interface.
func (o *O[T]) UnmarshalText(data []byte) error {
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		var t T
		o.V = t
		o.Valid = false
		return nil
	}

	switch any(o.V).(type) {
	case bool:
		var b null.Bool
		if err := b.UnmarshalText(data); err != nil {
			return err
		}
		o.V, o.Valid = any(b.Bool).(T), true
		return nil
	case int:
		var i null.Int
		if err := i.UnmarshalText(data); err != nil {
			return err
		}
		o.V, o.Valid = any(int(i.Int64)).(T), true
		return nil
	case string:
		var s null.String
		if err := s.UnmarshalText(data); err != nil {
			return err
		}
		o.V, o.Valid = any(s.String).(T), true
		return nil
	default:
		t, ok := any(&o.V).(encoding.TextUnmarshaler)
		if !ok {
			return fmt.Errorf("unsupported type for UnmarshalText: %T", t)
		}
		if err := t.UnmarshalText(data); err != nil {
			return err
		}
		o.Valid = true
		return nil
	}
}

// MarshalText implements encoding.Marshaler interface.
func (o O[T]) MarshalText() ([]byte, error) {
	if !o.Valid {
		return []byte{}, nil
	}

	switch v := any(o.V).(type) {
	case bool:
		if v {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case int:
		return []byte(strconv.FormatInt(int64(v), 10)), nil
	case string:
		return []byte(v), nil
	default:
		t, ok := v.(encoding.TextMarshaler)
		if !ok {
			return nil, fmt.Errorf("unsupported type for MarshalText: %T", t)
		}
		return t.MarshalText()
	}
}

// Scan implements sql.Scanner interface.
func (o *O[T]) Scan(src any) error {
	switch any(o.V).(type) {
	case bool:
		var b sql.NullBool
		if err := b.Scan(src); err != nil {
			return err
		}
		o.V, o.Valid = any(b.Bool).(T), b.Valid
		return nil
	case int:
		var i sql.NullInt64
		if err := i.Scan(src); err != nil {
			return err
		}
		o.V, o.Valid = any(int(i.Int64)).(T), i.Valid
		return nil
	case string:
		var s sql.NullString
		if err := s.Scan(src); err != nil {
			return err
		}
		o.V, o.Valid = any(s.String).(T), s.Valid
		return nil
	case time.Time:
		var t sql.NullTime
		if err := t.Scan(src); err != nil {
			return err
		}
		o.V, o.Valid = any(t.Time).(T), t.Valid
		return nil
	default:
		s, ok := any(&o.V).(sql.Scanner)
		if !ok {
			return fmt.Errorf("unsupported type for Scan: %T", o.V)
		}
		if err := s.Scan(src); err != nil {
			return err
		}
		o.Valid = true
		return nil
	}
}

// Value implements driver.Valuer interface.
func (o O[T]) Value() (driver.Value, error) {
	if !o.Valid {
		return nil, nil
	}
	switch v := any(o.V).(type) {
	case int:
		return int64(v), nil
	case driver.Valuer:
		return v.Value()
	}
	return o.V, nil
}
