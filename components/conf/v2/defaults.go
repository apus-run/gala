package conf

import "reflect"

// Go does not expose private struct state through reflection. Copy bindable
// fields and treat ignored/private state as opaque, as with ordinary Go assignment.
func cloneDefault(v reflect.Value, seen map[visit]reflect.Value) reflect.Value {
	t := v.Type()
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(t).Elem()
		out.Set(cloneDefault(v.Elem(), seen))
		return out
	case reflect.Struct:
		out := reflect.New(t).Elem()
		out.Set(v)
		for f := range t.Fields() {
			if fieldName(f) != "" {
				out.FieldByIndex(f.Index).Set(cloneDefault(v.FieldByIndex(f.Index), seen))
			}
		}
		return out
	case reflect.Array:
		out := reflect.New(t).Elem()
		for i := range v.Len() {
			out.Index(i).Set(cloneDefault(v.Index(i), seen))
		}
		return out
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return v
		}
		id := visit{typ: t, pointer: v.Pointer()}
		if v.Kind() == reflect.Slice {
			id.length = v.Len()
		}
		if previous, ok := seen[id]; ok {
			return previous
		}
		switch v.Kind() {
		case reflect.Pointer:
			out := reflect.New(t.Elem()).Convert(t)
			seen[id] = out
			out.Elem().Set(cloneDefault(v.Elem(), seen))
			return out
		case reflect.Map:
			out := reflect.MakeMapWithSize(t, v.Len())
			seen[id] = out
			for iter := v.MapRange(); iter.Next(); {
				out.SetMapIndex(iter.Key(), cloneDefault(iter.Value(), seen))
			}
			return out
		default:
			out := reflect.MakeSlice(t, v.Len(), v.Len())
			seen[id] = out
			for i := range v.Len() {
				out.Index(i).Set(cloneDefault(v.Index(i), seen))
			}
			return out
		}
	default:
		return v
	}
}
