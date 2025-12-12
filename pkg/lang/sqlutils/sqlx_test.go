package sqlutil

import (
	"reflect"
	"testing"
)

type User struct {
	ID   int
	Name string
	Age  int
}

func TestDriverValue_VariousTypes(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "int",
			run: func(t *testing.T) {
				x := 42
				v := DriverValue(x)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, x) {
					t.Fatalf("got %v (%T), want %v (%T)", got, got, x, x)
				}
			},
		},
		{
			name: "string",
			run: func(t *testing.T) {
				s := "hello"
				v := DriverValue(s)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, s) {
					t.Fatalf("got %v (%T), want %v (%T)", got, got, s, s)
				}
			},
		},
		{
			name: "struct",
			run: func(t *testing.T) {
				u := User{ID: 1, Name: "Alice", Age: 30}
				v := DriverValue(u)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, u) {
					t.Fatalf("got %#v, want %#v", got, u)
				}
			},
		},
		{
			name: "map",
			run: func(t *testing.T) {
				m := map[string]string{"a": "b"}
				v := DriverValue(m)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, m) {
					t.Fatalf("got %#v, want %#v", got, m)
				}
			},
		},
		{
			name: "slice",
			run: func(t *testing.T) {
				slice := []int{1, 2, 3}
				v := DriverValue(slice)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if !reflect.DeepEqual(got, slice) {
					t.Fatalf("got %#v, want %#v", got, slice)
				}
			},
		},
		{
			name: "pointer",
			run: func(t *testing.T) {
				u := &User{ID: 2, Name: "Bob", Age: 25}
				v := DriverValue(u)
				got, err := v.Value()
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != u {
					t.Fatalf("got %p, want %p", got, u)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}
func TestJSONColumn_Value(t *testing.T) {
	t.Run("invalid returns nil", func(t *testing.T) {
		j := JSONColumn[map[string]string]{Valid: false}
		got, err := j.Value()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})

	t.Run("map marshals to json string", func(t *testing.T) {
		j := JSONColumn[map[string]string]{Val: map[string]string{"a": "b"}, Valid: true}
		got, err := j.Value()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s, ok := got.(string)
		if !ok {
			t.Fatalf("got type %T, want string", got)
		}
		const want = `{"a":"b"}`
		if s != want {
			t.Fatalf("got %s, want %s", s, want)
		}
	})

	t.Run("struct marshals to json string", func(t *testing.T) {
		u := User{ID: 1, Name: "Alice", Age: 30}
		j := JSONColumn[User]{Val: u, Valid: true}
		got, err := j.Value()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		s, ok := got.(string)
		if !ok {
			t.Fatalf("got type %T, want string", got)
		}
		const want = `{"ID":1,"Name":"Alice","Age":30}`
		if s != want {
			t.Fatalf("got %s, want %s", s, want)
		}
	})
}

func TestJSONColumn_Scan(t *testing.T) {
	t.Run("scan []byte into map", func(t *testing.T) {
		var j JSONColumn[map[string]string]
		err := j.Scan([]byte(`{"a":"b"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !j.Valid {
			t.Fatalf("expected Valid true")
		}
		if !reflect.DeepEqual(j.Val, map[string]string{"a": "b"}) {
			t.Fatalf("got %#v, want %#v", j.Val, map[string]string{"a": "b"})
		}
	})

	t.Run("scan string into struct", func(t *testing.T) {
		var j JSONColumn[User]
		err := j.Scan(`{"ID":2,"Name":"Bob","Age":25}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !j.Valid {
			t.Fatalf("expected Valid true")
		}
		want := User{ID: 2, Name: "Bob", Age: 25}
		if !reflect.DeepEqual(j.Val, want) {
			t.Fatalf("got %#v, want %#v", j.Val, want)
		}
	})

	t.Run("scan nil does not modify", func(t *testing.T) {
		original := User{ID: 9, Name: "Keep", Age: 99}
		j := JSONColumn[User]{Val: original, Valid: true}
		if err := j.Scan(nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !j.Valid {
			t.Fatalf("expected Valid true after nil scan")
		}
		if !reflect.DeepEqual(j.Val, original) {
			t.Fatalf("value changed after nil scan: got %#v, want %#v", j.Val, original)
		}
	})

	t.Run("scan unsupported type returns error", func(t *testing.T) {
		var j JSONColumn[map[string]string]
		if err := j.Scan(123); err == nil {
			t.Fatalf("expected error for unsupported scan type")
		}
	})

	t.Run("scan invalid json returns error", func(t *testing.T) {
		var j JSONColumn[map[string]string]
		if err := j.Scan([]byte("not json")); err == nil {
			t.Fatalf("expected json unmarshal error")
		}
	})
}
