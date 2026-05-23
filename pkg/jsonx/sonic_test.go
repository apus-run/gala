package json

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJson(t *testing.T) {
	v := map[string]any{
		"name": "evaluator",
		"age":  123,
	}

	b, err := Marshal(v)
	assert.Nil(t, err)

	tar := map[string]any{}
	err = Unmarshal(b, &tar)
	assert.Nil(t, err)
	assert.Equal(t, "evaluator", tar["name"])

	r := bytes.NewReader(b)

	tar2 := map[string]any{}
	err = Decode(r, &tar2)
	assert.Nil(t, err)
	assert.Equal(t, "evaluator", tar2["name"])

	ok := Valid(b)
	assert.True(t, ok)
}

func TestJsonify(t *testing.T) {
	v := map[string]any{
		"name": "evaluator",
		"age":  123,
	}
	res := Jsonify(v)

	tar := map[string]any{}
	err := Unmarshal([]byte(res), &tar)
	assert.Nil(t, err)
	assert.Equal(t, "evaluator", tar["name"])
}
