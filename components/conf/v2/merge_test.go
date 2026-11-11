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
			out, err := new(layerMerger).copyValue(tc.value)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if err == nil && !sameJSON(out, tc.value) {
				t.Fatal("copy changed values")
			}
		})
	}
	out, err := new(layerMerger).copyValue([]any{shared, shared})
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
		_, err := new(layerMerger).copyValue(value)
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
	var layers layerMerger
	if err := layers.add(base); err != nil {
		t.Fatal(err)
	}
	if err := layers.add(layer); err != nil {
		t.Fatal(err)
	}
	layer["object"].(map[string]any)["replace"].([]int)[0] = 9
	object := layers.values["object"].(map[string]any)
	if object["keep"] != 1 || !reflect.DeepEqual(object["replace"], []any{3}) {
		t.Fatalf("merge=%v", layers.values)
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
		copied, copyErr := new(layerMerger).copyValue(value)
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

func TestExpandedNodeBudget(t *testing.T) {
	var layers layerMerger
	// Root + slice + 65,534 string elements consumes the entire budget.
	if err := layers.add(map[string]any{"items": make([]string, maxExpandedNodes-2)}); err != nil {
		t.Fatal(err)
	}
	if err := layers.add(nil); err != nil {
		t.Fatal(err)
	}
	if err := layers.add(map[string]any{}); err != errNodeLimit {
		t.Fatalf("extra root: %v", err)
	}

	shared := map[string]any{"port": 1}
	for range 30 {
		shared = map[string]any{"left": shared, "right": shared}
	}
	for _, layer := range []map[string]any{shared, {"items": make([]int, maxExpandedNodes)}} {
		var layers layerMerger
		if err := layers.add(map[string]any{"keep": 1}); err != nil {
			t.Fatal(err)
		}
		if err := layers.add(layer); err != errNodeLimit || !reflect.DeepEqual(layers.values, map[string]any{"keep": 1}) {
			t.Fatalf("partial merge or wrong error: %v", err)
		}
	}
}

// FuzzMerge starts with an independently copied lower layer, then applies an
// object or scalar override. A failed overlay must leave that layer intact.
func FuzzMerge(f *testing.F) {
	f.Add(`{"leaf":[1],"keep":"yes"}`, `{"leaf":[2]}`)
	f.Add(`{"leaf":1}`, `[null]`)
	f.Add(`{"keep":"old"}`, `false`)
	f.Add(`false`, `{"enabled":true}`)
	f.Fuzz(func(t *testing.T, lower, higher string) {
		if len(lower)+len(higher) > 65536 {
			t.Skip()
		}
		var low, high any
		if json.Unmarshal([]byte(lower), &low) != nil || json.Unmarshal([]byte(higher), &high) != nil {
			return
		}
		var layers layerMerger
		if layers.add(map[string]any{"root": low}) != nil {
			return
		}
		before, _ := json.Marshal(layers.values)
		err := layers.add(map[string]any{"root": high})
		if err != nil {
			after, _ := json.Marshal(layers.values)
			if string(after) != string(before) {
				t.Fatal("partial overlay")
			}
			return
		}
		want := overlayValue(low, high)
		if !reflect.DeepEqual(layers.values["root"], want) {
			t.Fatal("incorrect precedence")
		}
		expected, _ := json.Marshal(layers.values)
		mutateCollections(low)
		mutateCollections(high)
		got, _ := json.Marshal(layers.values)
		if string(expected) != string(got) {
			t.Fatal("aliased source")
		}
	})
}
func overlayValue(low, high any) any {
	a, ok := low.(map[string]any)
	b, object := high.(map[string]any)
	if !ok || !object {
		return high
	}
	for k, v := range b {
		a[k] = overlayValue(a[k], v)
	}
	return a
}
