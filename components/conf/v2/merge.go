package conf

import (
	"errors"
	"reflect"
)

// mergeInto recursively merges objects and replaces other values. Every layer
// is checked before later layers can hide nulls, and mutable data is copied.
func mergeInto(dst, layer map[string]any) error {
	if layer == nil {
		return nil
	}
	copied, err := copyValue(reflect.ValueOf(layer))
	if err != nil {
		return err
	}
	mergeCopied(dst, copied.(map[string]any))
	return nil
}

// mergeCopied consumes an already validated, privately owned layer.
func mergeCopied(dst, layer map[string]any) {
	for k, value := range layer {
		lm, lIsMap := value.(map[string]any)
		dm, dIsMap := dst[k].(map[string]any)
		if lIsMap && dIsMap {
			mergeCopied(dm, lm)
		} else {
			dst[k] = value
		}
	}
}

var (
	errNull       = errors.New("explicit null is not allowed")
	errSourceType = errors.New("unsupported source value; use scalars and collections")
	errCycle      = errors.New("cyclic source values are not allowed")
	errDepth      = errors.New("source nesting exceeds 128 levels")
)

// visit tracks only ancestors, so shared but acyclic collections remain valid.
type visit struct {
	typ     reflect.Type
	pointer uintptr
	length  int
}

// Normalize objects and sequences at the boundary, so merge, hooks and binding
// share one representation regardless of the source's concrete collection types.
func copyValue(v reflect.Value) (any, error) {
	return copyNested(v, make(map[visit]bool), 0)
}

func copyNested(v reflect.Value, active map[visit]bool, depth int) (any, error) {
	if depth > 128 {
		return nil, errDepth
	}

	if !v.IsValid() {
		return nil, errNull
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, errNull
		}
		return copyNested(v.Elem(), active, depth)
	}
	if v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		if v.IsNil() {
			return nil, errNull
		}
		key := visit{typ: v.Type(), pointer: v.Pointer(), length: v.Len()}
		if active[key] {
			return nil, errCycle
		}
		active[key] = true
		defer delete(active, key)
	}
	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return nil, errSourceType
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			value, err := copyNested(iter.Value(), active, depth+1)
			if err != nil {
				return nil, err
			}
			out[iter.Key().String()] = value
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := range v.Len() {
			value, err := copyNested(v.Index(i), active, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case reflect.Struct, reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return nil, errSourceType
	}
	return v.Interface(), nil
}
