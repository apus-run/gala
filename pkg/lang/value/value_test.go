package value

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZero(t *testing.T) {
	assert.Zero(t, Zero[bool]())
	assert.Zero(t, Zero[int]())
	assert.Zero(t, Zero[*int]())
	assert.Zero(t, Zero[string]())
	assert.Zero(t, Zero[interface{}]())
	assert.Zero(t, Zero[*interface{}]())
}
