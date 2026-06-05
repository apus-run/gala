package rdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestRedis(t *testing.T) {
	cli := NewTestRedis(t)
	ctx := context.TODO()

	key := `resource:1:name`
	require.NoError(t, cli.Set(ctx, key, `alice`, time.Hour).Err())

	got, err := cli.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, `alice`, got)
}

func TestNewRDBFromConfigNilOptions(t *testing.T) {
	provider, err := NewRDBFromConfig(nil)

	require.Nil(t, provider)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNilOptions))
}

func TestNewClientCompatibility(t *testing.T) {
	provider, err := NewClient(&Options{})
	require.NoError(t, err)

	client, ok := Unwrap(provider)
	require.True(t, ok)
	t.Cleanup(func() {
		_ = Close(client)
	})
}
