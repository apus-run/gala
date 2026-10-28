package rdbtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/apus-run/gala/components/rdb/rdbtest"
)

func TestNew(t *testing.T) {
	cli := rdbtest.New(t)
	ctx := context.Background()

	key := `resource:1:name`
	require.NoError(t, cli.Set(ctx, key, `alice`, time.Hour).Err())

	got, err := cli.Get(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, `alice`, got)
}
