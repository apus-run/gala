package conf

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

// StrictMerge is an Options.Merge policy. It rejects changes of concrete value
// type at an existing key before applying the ordinary recursive overlay.
// Both arguments are private, normalized layers supplied by OpenWith.
// A nil destination cannot receive an overlay.
func StrictMerge(dst, src map[string]any) error {
	if dst == nil {
		return errors.New("nil merge destination")
	}
	if err := checkMergeTypes(dst, src); err != nil {
		return err
	}
	mergeCopied(dst, src)
	return nil
}

func checkMergeTypes(dst, src map[string]any) error {
	for k, v := range src {
		old, ok := dst[k]
		if !ok {
			continue
		}
		if reflect.TypeOf(old) != reflect.TypeOf(v) {
			return errors.New("conflicting configuration types")
		}
		if child, ok := v.(map[string]any); ok {
			if err := checkMergeTypes(old.(map[string]any), child); err != nil {
				return err
			}
		}
	}
	return nil
}

// Expand is an opt-in Options.Resolve function. It expands ${a.b} and
// ${a.b:fallback} against the merged configuration, including references in
// lists. A whole reference keeps its scalar type. Missing references without
// a fallback, non-scalars, malformed placeholders and cycles return fixed
// errors. Use $${ for a literal ${. Fallbacks are literal strings. Expansion is bounded to 128 reference
// levels and 16 MiB of generated text per call.
func Expand(root map[string]any) error {
	if len(root) == 0 {
		return nil
	}
	owned, err := new(layerMerger).copyValue(root)
	if err != nil {
		return err
	}
	normalized := owned.(map[string]any)
	r := expander{tree: newTree(normalized), active: make(map[string]bool), memo: make(map[string]expansion), remaining: 16 << 20}
	// Evaluate against the unchanged private tree first. Keeping only the
	// replacements avoids cloning every collection again.
	replacements := make(map[string]any)
	if err := visitTemplates(normalized, func(value any, text string) (any, error) {
		result, err := r.scalar(value)
		if err == nil {
			replacements[text] = result.value
		}
		return value, err
	}); err != nil {
		return err
	}
	if err := visitTemplates(normalized, func(_ any, text string) (any, error) { return replacements[text], nil }); err != nil {
		return err
	}
	for k, v := range normalized {
		root[k] = v
	}
	return nil
}

type expander struct {
	tree      Tree
	active    map[string]bool
	memo      map[string]expansion
	remaining int
}

// depth counts the longest chain of references used to produce value.
type expansion struct {
	value any
	depth int
}

func visitTemplates(node any, transform func(any, string) (any, error)) error {
	switch v := node.(type) {
	case map[string]any:
		for k, item := range v {
			if text, ok := expansionText(item); ok && strings.Contains(text, "${") {
				next, err := transform(item, text)
				if err != nil {
					return err
				}
				v[k] = next
			} else if err := visitTemplates(item, transform); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range v {
			if text, ok := expansionText(item); ok && strings.Contains(text, "${") {
				next, err := transform(item, text)
				if err != nil {
					return err
				}
				v[i] = next
			} else if err := visitTemplates(item, transform); err != nil {
				return err
			}
		}
	}
	return nil
}

const maxReferenceDepth = 128

func (r *expander) reference(expr string) (expansion, error) {
	key, fallback, optional := strings.Cut(expr, ":")
	if key == "" {
		return expansion{}, errors.New("empty placeholder reference")
	}
	if r.active[key] {
		return expansion{}, errors.New("cyclic placeholder reference")
	}
	if len(r.active) >= maxReferenceDepth {
		return expansion{}, errors.New("placeholder reference depth exceeded")
	}
	if value, ok := r.memo[key]; ok {
		if len(r.active)+value.depth > maxReferenceDepth {
			return expansion{}, errors.New("placeholder reference depth exceeded")
		}
		return value, nil
	}
	value, ok := r.tree.find(strings.Split(key, "."))
	if !ok {
		if optional {
			return expansion{value: fallback, depth: 1}, nil
		}
		return expansion{}, ErrNotFound
	}
	switch value.(type) {
	case map[string]any, []any:
		return expansion{}, errors.New("placeholder requires a scalar")
	}
	r.active[key] = true
	result, err := r.scalar(value)
	delete(r.active, key)
	if err != nil {
		return expansion{}, err
	}
	result.depth++
	r.memo[key] = result
	return result, nil
}

// json.Number has string kind but represents a numeric token.
func expansionText(value any) (string, bool) {
	if _, numeric := value.(json.Number); numeric {
		return "", false
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

func (r *expander) scalar(value any) (expansion, error) {
	text, ok := expansionText(value)
	if !ok || !strings.Contains(text, "${") {
		return expansion{value: value}, nil
	}
	var depth int
	whole := true
	var out strings.Builder
	for {
		literal, rest, found := strings.Cut(text, "${")
		if found && strings.HasSuffix(literal, "$") {
			if err := r.write(&out, strings.TrimSuffix(literal, "$")); err != nil {
				return expansion{}, err
			}
			if err := r.write(&out, "${"); err != nil {
				return expansion{}, err
			}
			whole = false
			text = rest
			continue
		}
		if err := r.write(&out, literal); err != nil {
			return expansion{}, err
		}
		if !found {
			return expansion{value: out.String(), depth: depth}, nil
		}
		expr, tail, closed := strings.Cut(rest, "}")
		if !closed || strings.Contains(expr, "${") {
			return expansion{}, errors.New("invalid placeholder")
		}
		ref, err := r.reference(expr)
		if err != nil {
			return expansion{}, err
		}
		depth = max(depth, ref.depth)
		if whole && literal == "" && tail == "" {
			if str, ok := expansionText(ref.value); ok {
				if len(str) > r.remaining {
					return expansion{}, errors.New("placeholder expansion limit exceeded")
				}
				r.remaining -= len(str)
			}
			return ref, nil
		}
		if err := r.write(&out, scalarText(ref.value)); err != nil {
			return expansion{}, err
		}
		whole = false
		text = tail
	}
}

func (r *expander) write(out *strings.Builder, text string) error {
	if len(text) > r.remaining {
		return errors.New("placeholder expansion limit exceeded")
	}
	r.remaining -= len(text)
	out.WriteString(text)
	return nil
}

func scalarText(value any) string {
	if n, ok := value.(json.Number); ok {
		return string(n)
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())
	default:
		return v.String()
	}
}
