package token

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHash(t *testing.T) {
	h1 := Hash("jwt-token-a")
	h2 := Hash("jwt-token-a")
	h3 := Hash("jwt-token-b")

	assert.Len(t, h1, 64)
	assert.Equal(t, h1, h2)
	assert.NotEqual(t, h1, h3)
}
