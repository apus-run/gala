package ginx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	assert.Equal(t, "dev", o.mode)
	assert.Equal(t, "localhost", o.host)
	assert.Equal(t, "8080", o.port)
	assert.Equal(t, 5, o.maxPingCount)
	assert.Empty(t, o.beforeStart)
	assert.Empty(t, o.afterStart)
	assert.Empty(t, o.afterStop)
}

func TestNewOptions(t *testing.T) {
	noop := func(context.Context) error { return nil }
	o, err := NewOptions(
		WithMode("PROD"),
		WithAddr("0.0.0.0"),
		WithPort("9090"),
		WithMaxPingCount(3),
		BeforeStart(noop),
		AfterStart(noop),
		AfterStop(noop),
	)
	require.NoError(t, err)
	assert.Equal(t, "PROD", o.mode, "the caller's spelling is kept; validation is case-insensitive")
	assert.Equal(t, "0.0.0.0", o.host)
	assert.Equal(t, "9090", o.port)
	assert.Equal(t, 3, o.maxPingCount)
	require.Len(t, o.beforeStart, 1)
	require.Len(t, o.afterStart, 1)
	require.Len(t, o.afterStop, 1)
}

func TestNewOptionsReturnsFirstError(t *testing.T) {
	o, err := NewOptions(WithMode("staging"), WithPort("9090"))
	assert.Error(t, err)
	assert.Nil(t, o)
}

func TestApplyStopsAtFirstError(t *testing.T) {
	o := DefaultOptions()
	err := o.Apply(WithPort("9090"), WithMode("staging"), WithAddr("ignored"))
	require.Error(t, err)
	assert.Equal(t, "9090", o.port)
	assert.Equal(t, "localhost", o.host, "options after the failing one are not applied")
}

func TestOptionValidation(t *testing.T) {
	tests := []struct {
		name    string
		opt     Option
		wantErr bool
	}{
		{name: "dev", opt: WithMode("dev")},
		{name: "prod case-insensitive", opt: WithMode("Prod")},
		{name: "unknown mode", opt: WithMode("staging"), wantErr: true},
		{name: "empty host", opt: WithAddr(""), wantErr: true},
		{name: "empty port", opt: WithPort(""), wantErr: true},
		{name: "zero ping", opt: WithMaxPingCount(0), wantErr: true},
		{name: "negative ping", opt: WithMaxPingCount(-1), wantErr: true},
		{name: "nil before start", opt: BeforeStart(nil), wantErr: true},
		{name: "nil after start", opt: AfterStart(nil), wantErr: true},
		{name: "nil after stop", opt: AfterStop(nil), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opt(DefaultOptions())
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHooksAppendInOrder(t *testing.T) {
	var got []string
	o := DefaultOptions()
	require.NoError(t, o.Apply(
		BeforeStart(func(context.Context) error { got = append(got, "first"); return nil }),
		BeforeStart(func(context.Context) error { got = append(got, "second"); return nil }),
	))
	for _, fn := range o.beforeStart {
		require.NoError(t, fn(context.Background()))
	}
	assert.Equal(t, []string{"first", "second"}, got)
}
