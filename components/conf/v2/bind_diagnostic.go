package conf

import (
	"reflect"
	"strings"
)

// schemaFailure runs only after binding fails. It locates a strict conversion
// failure using declared fields and the same pure hook as the decoder. It does
// not parse dependency errors or traverse dynamic map keys/collection elements.
func schemaFailure(data any, target reflect.Type, path string) (string, error) {
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if _, err := strictHook(reflect.TypeOf(data), target, data); err != nil {
		return path, err
	}
	object, ok := data.(map[string]any)
	if !ok || target.Kind() != reflect.Struct {
		return "", nil
	}
	for field := range target.Fields() {
		if !field.IsExported() {
			continue
		}
		name, options, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
		// These options alter field routing; omit paths rather than guessing.
		if strings.Contains(options, "squash") || strings.Contains(options, "remain") {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if !publicFieldName(name) {
			continue
		}
		value, exists := object[name]
		if !exists {
			continue
		}
		next := name
		if path != "" {
			next = path + "." + name
		}
		if found, err := schemaFailure(value, field.Type, next); err != nil {
			return found, err
		}
	}
	return "", nil
}

// Accept ordinary schema identifiers only; tags can contain control characters
// or punctuation that would make an error ambiguous or unsafe to display.
func publicFieldName(name string) bool {
	if name == "" || name == "-" {
		return false
	}
	for i, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			continue
		}
		if i > 0 && (c >= '0' && c <= '9' || c == '-') {
			continue
		}
		return false
	}
	return true
}
