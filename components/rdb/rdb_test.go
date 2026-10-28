package rdb

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNilOptions(t *testing.T) {
	p, err := New(nil)

	require.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilOptions)
}

func TestNewFromURL(t *testing.T) {
	m := miniredis.RunT(t)

	p, err := NewFromURL("redis://" + m.Addr() + "/0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NoError(t, p.DB(context.Background()).Ping(context.Background()).Err())
}

func TestNewFromURLInvalid(t *testing.T) {
	p, err := NewFromURL("http://localhost:6379")

	require.Nil(t, p)
	require.Error(t, err)
}

func TestNewFromClientNil(t *testing.T) {
	p, err := NewFromClient(nil)

	require.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilClient)
}

func TestNewFromClient(t *testing.T) {
	m := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: m.Addr()})

	p, err := NewFromClient(cli)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	got, ok := Unwrap(p)
	require.True(t, ok)
	assert.Same(t, cli, got)
	require.NoError(t, p.DB(context.Background()).Ping(context.Background()).Err())
}

func TestNewFromUniversalClientNil(t *testing.T) {
	p, err := NewFromUniversalClient(nil)

	require.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilClient)
}

func TestNewFromUniversalClient(t *testing.T) {
	m := miniredis.RunT(t)
	cli := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{m.Addr()}})

	p, err := NewFromUniversalClient(cli)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NoError(t, p.DB(context.Background()).Ping(context.Background()).Err())
}

// fakeProvider is a Provider that is not backed by a *Client.
type fakeProvider struct{ Provider }

func TestUnwrap(t *testing.T) {
	t.Run("not a provider", func(t *testing.T) {
		got, ok := Unwrap(fakeProvider{})
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("nil provider", func(t *testing.T) {
		got, ok := Unwrap(nil)
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("not a standalone client", func(t *testing.T) {
		cli := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
		p, err := NewFromUniversalClient(cli)
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, ok := Unwrap(p)
		assert.False(t, ok)
	})
}

func TestConnect(t *testing.T) {
	m := miniredis.RunT(t)

	p, err := Connect(context.Background(), &Options{Addr: m.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NoError(t, p.DB(context.Background()).Ping(context.Background()).Err())
}

func TestConnectNilOptions(t *testing.T) {
	p, err := Connect(context.Background(), nil)

	require.Nil(t, p)
	assert.ErrorIs(t, err, ErrNilOptions)
}

func TestConnectUnreachable(t *testing.T) {
	m := miniredis.RunT(t)
	addr := m.Addr()
	m.Close()

	p, err := Connect(context.Background(), &Options{Addr: addr})

	require.Nil(t, p)
	require.Error(t, err)
}

func TestConnectCanceledContext(t *testing.T) {
	m := miniredis.RunT(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p, err := Connect(ctx, &Options{Addr: m.Addr()})

	require.Nil(t, p)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestClose(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		assert.NoError(t, Close(nil))
	})

	t.Run("twice", func(t *testing.T) {
		cli := redis.NewClient(&redis.Options{})
		require.NoError(t, Close(cli))
		assert.NoError(t, Close(cli))
	})
}

func TestProviderCloseTwice(t *testing.T) {
	m := miniredis.RunT(t)
	p, err := New(&Options{Addr: m.Addr()})
	require.NoError(t, err)

	require.NoError(t, p.Close())
	assert.NoError(t, p.Close())

	err = p.DB(context.Background()).Ping(context.Background()).Err()
	assert.True(t, IsClosedError(err))
}

func TestIsNilError(t *testing.T) {
	assert.False(t, IsNilError(nil))
	assert.False(t, IsNilError(errors.New("boom")))
	assert.True(t, IsNilError(redis.Nil))
	assert.True(t, IsNilError(fmt.Errorf("wrap: %w", redis.Nil)))
}

func TestIsClosedError(t *testing.T) {
	assert.False(t, IsClosedError(nil))
	assert.False(t, IsClosedError(errors.New("boom")))
	assert.True(t, IsClosedError(redis.ErrClosed))
	assert.True(t, IsClosedError(fmt.Errorf("wrap: %w", redis.ErrClosed)))
}

func TestNewScript(t *testing.T) {
	m := miniredis.RunT(t)
	p, err := New(&Options{Addr: m.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	s := NewScript(`return redis.call('SET', KEYS[1], ARGV[1])`)
	require.NoError(t, s.Run(context.Background(), p.DB(context.Background()), []string{"k"}, "v").Err())

	got, err := m.Get("k")
	require.NoError(t, err)
	assert.Equal(t, "v", got)
}
