package conf

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestCopySourceValueBoundaries(t *testing.T) {
	cyclicMap := map[string]any{}
	cyclicMap["self"] = cyclicMap
	cyclicSlice := make([]any, 1)
	cyclicSlice[0] = cyclicSlice
	shared := map[string]any{"items": []int{1, 2}}
	for _, tc := range []struct {
		name  string
		value any
		want  error
	}{
		{"struct slice", struct{ Values []string }{[]string{"old"}}, errSourceType},
		{"nested struct", []any{struct{ Values map[string]int }{map[string]int{"key": 1}}}, errSourceType},
		{"map cycle", cyclicMap, errCycle},
		{"slice cycle", cyclicSlice, errCycle},
		{"shared subtree", []any{shared, shared}, nil},
		{"array", [2][]int{{1}, {2}}, nil},
		{"typed map", map[string][]int{"key": {1}}, nil},
		{"non-string keys", map[int]string{1: "a"}, errSourceType},
		{"nil map", map[string]int(nil), errNull},
		{"nil slice", []int(nil), errNull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := copyValue(reflect.ValueOf(tc.value))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if err == nil && !sameJSON(out, tc.value) {
				t.Fatal("copy changed values")
			}
		})
	}
	out, err := copyValue(reflect.ValueOf([]any{shared, shared}))
	if err != nil {
		t.Fatal(err)
	}
	shared["items"].([]int)[0] = 9
	copies := out.([]any)
	if copies[0].(map[string]any)["items"].([]any)[0] != 1 {
		t.Fatal("source aliases copy")
	}
	copies[0].(map[string]any)["items"].([]any)[0] = 7
	if copies[1].(map[string]any)["items"].([]any)[0] != 1 {
		t.Fatal("shared subtree was not copied independently")
	}
}

func TestCopyDepthBoundary(t *testing.T) {
	for _, depth := range []int{128, 129} {
		var value any = "leaf"
		for range depth {
			value = map[string]any{"child": value}
		}
		_, err := copyValue(reflect.ValueOf(value))
		if depth == 128 && err != nil {
			t.Fatal(err)
		}
		if depth == 129 && !errors.Is(err, errDepth) {
			t.Fatalf("depth error=%v", err)
		}
	}
}

func TestMergeCopiesAndReplaces(t *testing.T) {
	base := map[string]any{"object": map[string]any{"keep": 1, "replace": []int{1, 2}}}
	layer := map[string]any{"object": map[string]any{"replace": []int{3}}}
	dst := map[string]any{}
	if err := mergeInto(dst, base); err != nil {
		t.Fatal(err)
	}
	if err := mergeInto(dst, layer); err != nil {
		t.Fatal(err)
	}
	layer["object"].(map[string]any)["replace"].([]int)[0] = 9
	object := dst["object"].(map[string]any)
	if object["keep"] != 1 || !reflect.DeepEqual(object["replace"], []any{3}) {
		t.Fatalf("merge=%v", dst)
	}
	if !reflect.DeepEqual(base["object"].(map[string]any)["replace"], []int{1, 2}) {
		t.Fatal("modified base")
	}
}

func FuzzCopyValue(f *testing.F) {
	for _, seed := range []string{`{"a":[1,"x",true]}`, `{"a":null}`, `[[[]]]`, `{"a":{"b":1}}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			t.Skip()
		}
		var value any
		if json.Unmarshal(raw, &value) != nil {
			return
		}
		before, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		copied, copyErr := copyValue(reflect.ValueOf(value))
		after, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("copy mutated input")
		}
		if copyErr != nil {
			if !errors.Is(copyErr, errNull) && !errors.Is(copyErr, errDepth) {
				t.Fatalf("unexpected error: %v", copyErr)
			}
			return
		}
		mutateCollections(value)
		got, err := json.Marshal(copied)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(before) {
			t.Fatal("copy changed or aliases source")
		}
	})
}

func mutateCollections(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			mutateCollections(child)
			delete(v, key)
		}
	case []any:
		for i, child := range v {
			mutateCollections(child)
			v[i] = nil
		}
	}
}

func sameJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && string(left) == string(right)
}
