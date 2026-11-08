package conf

import (
	"errors"
	"reflect"
)

// mergeInto recursively merges objects and replaces other values. Every layer
// is checked before later layers can hide nulls, and mutable data is copied.
func mergeInto(dst, layer map[string]any) error {
	for k, v := range layer {
		copied, err := copyValue(reflect.ValueOf(v))
		if err != nil {
			return err
		}
		value := copied.Interface()
		lm, lIsMap := value.(map[string]any)
		dm, dIsMap := dst[k].(map[string]any)
		if lIsMap && dIsMap {
			if err := mergeInto(dm, lm); err != nil {
				return err
			}
		} else {
			dst[k] = value
		}
	}
	return nil
}

var errNull = errors.New("explicit null is not allowed")

func copyValue(v reflect.Value) (reflect.Value, error) {
	if !v.IsValid() {
		return reflect.Value{}, errNull
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Value{}, errNull
		}
		inner, err := copyValue(v.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out, nil
	case reflect.Map:
		if v.IsNil() {
			return reflect.Value{}, errNull
		}
		if v.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, errTypeMismatch
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			value, err := copyValue(iter.Value())
			if err != nil {
				return reflect.Value{}, err
			}
			out.SetMapIndex(iter.Key(), value)
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		var out reflect.Value
		if v.Kind() == reflect.Slice {
			if v.IsNil() {
				return reflect.Value{}, errNull
			}
			out = reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		} else {
			out = reflect.New(v.Type()).Elem()
		}
		for i := 0; i < v.Len(); i++ {
			value, err := copyValue(v.Index(i))
			if err != nil {
				return reflect.Value{}, err
			}
			out.Index(i).Set(value)
		}
		return out, nil
	case reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return reflect.Value{}, errTypeMismatch
	}
	return v, nil
}
