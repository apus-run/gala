// Package redisstore adapts an application-owned go-redis client to layercache.
package redisstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/apus-run/gala/components/layercache"
)

// Client is the subset used from redis.Client, ClusterClient or Ring. Configure
// the supplied client to honor context deadlines (ContextTimeoutEnabled) and use
// suitable network timeouts/retries. Store does not change or close the client.
type Client interface {
	// Process must honor Cmder.NoRetry. Standard go-redis Client, ClusterClient
	// and Ring do; wrappers must preserve this per-command property.
	Process(context.Context, redis.Cmder) error
}

// Store implements layercache.Remote over an application-owned Redis client.
type Store struct{ client Client }

var _ layercache.Remote = (*Store)(nil)

// New validates the client without opening connections or changing its options.
func New(client Client) (*Store, error) {
	if client == nil {
		return nil, errors.New("redisstore: client is required")
	}
	// Client may be an application function adapter or another named nilable
	// type, not only a pointer to a go-redis client.
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, errors.New("redisstore: client is required")
		}
	}
	return &Store{client: client}, nil
}

// One key also works with Redis Cluster. Pipeline GET + PTTL is not an atomic
// snapshot: another writer could change the value between those commands.
const getScript = `local value = redis.call('GET', KEYS[1])
if not value then return {false, -2} end
return {value, redis.call('PTTL', KEYS[1])}`

func (s *Store) Get(ctx context.Context, key string) (layercache.RemoteEntry, error) {
	started := time.Now()
	result, err := s.eval(ctx, getScript, key, false)
	if err != nil {
		return layercache.RemoteEntry{}, err
	}
	return parseSnapshot(result, started)
}

func parseSnapshot(result any, started time.Time) (layercache.RemoteEntry, error) {
	var zero layercache.RemoteEntry
	parts, ok := result.([]any)
	if !ok || len(parts) != 2 {
		return zero, fmt.Errorf("redisstore: malformed snapshot: %w", layercache.ErrCorrupt)
	}
	ms, ok := parts[1].(int64)
	if !ok {
		return zero, fmt.Errorf("redisstore: malformed PTTL: %w", layercache.ErrCorrupt)
	}
	switch {
	case ms == -2:
		return zero, layercache.ErrMiss
	case ms == -1:
		return zero, layercache.ErrInvalidTTL
	case ms < -2 || ms > math.MaxInt64/int64(time.Millisecond):
		return zero, layercache.ErrInvalidTTL
	case ms <= 1:
		return zero, layercache.ErrMiss
	}
	var data []byte
	switch value := parts[0].(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return zero, fmt.Errorf("redisstore: malformed value: %w", layercache.ErrCorrupt)
	}
	// Use request start, retaining its monotonic clock. Subtract one millisecond
	// for Redis expiration precision and all response latency, never restarting TTL.
	deadline := started.Add(time.Duration(ms-1) * time.Millisecond)
	if !time.Now().Before(deadline) {
		return zero, layercache.ErrMiss
	}
	return layercache.RemoteEntry{Data: data, ExpiresAt: deadline}, nil
}

func (s *Store) Set(ctx context.Context, key string, data []byte, ttl time.Duration) (time.Time, error) {
	ms := ttl / time.Millisecond
	if ms <= 0 {
		return time.Time{}, layercache.ErrInvalidTTL
	}
	// Redis stores expiration at millisecond precision. Subtract a millisecond
	// even for exact TTLs, and use request start so latency never renews L1 life.
	deadline := time.Now().Add((ms - 1) * time.Millisecond)
	if err := s.process(ctx, redis.NewStatusCmd(ctx, "set", key, data, "px", int64(ms))); err != nil {
		return time.Time{}, err
	}
	return deadline, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.process(ctx, redis.NewIntCmd(ctx, "del", key))
}

const conditionalScript = `local result = redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2], ARGV[3])
if result then return 1 else return 0 end`

func (s *Store) SetIf(ctx context.Context, key string, data []byte, ttl time.Duration, mode layercache.SetMode) (time.Time, bool, error) {
	if mode == layercache.SetAlways {
		deadline, err := s.Set(ctx, key, data, ttl)
		return deadline, err == nil, err
	}
	flag := "NX"
	if mode == layercache.SetIfPresent {
		flag = "XX"
	} else if mode != layercache.SetIfAbsent {
		return time.Time{}, false, layercache.ErrInvalidOptions
	}
	ms := ttl / time.Millisecond
	if ms <= 0 {
		return time.Time{}, false, layercache.ErrInvalidTTL
	}
	deadline := time.Now().Add((ms - 1) * time.Millisecond)
	result, err := s.eval(ctx, conditionalScript, key, true, data, int64(ms), flag)
	if err != nil {
		return time.Time{}, false, err
	}
	n, ok := result.(int64)
	if !ok || (n != 0 && n != 1) {
		return time.Time{}, false, layercache.ErrCorrupt
	}
	if n == 0 {
		return time.Time{}, false, nil
	}
	return deadline, true, nil
}

type pipeliner interface{ Pipeline() redis.Pipeliner }

func (s *Store) MGet(ctx context.Context, keys []string) (map[string]layercache.RemoteEntry, error) {
	out := make(map[string]layercache.RemoteEntry, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	client, ok := s.client.(pipeliner)
	if !ok {
		var errs error
		for _, key := range keys {
			v, err := s.Get(ctx, key)
			if err == nil {
				out[key] = v
			} else if !errors.Is(err, layercache.ErrMiss) {
				errs = errors.Join(errs, err)
			}
		}
		return out, errs
	}
	pipe := client.Pipeline()
	commands := make(map[string]*redis.Cmd, len(keys))
	started := time.Now()
	for _, key := range keys {
		commands[key] = pipe.Eval(ctx, getScript, []string{key})
	}
	_, errs := pipe.Exec(ctx)
	for key, cmd := range commands {
		result, err := cmd.Result()
		if err != nil {
			errs = errors.Join(errs, fmt.Errorf("redisstore: get %q: %w", key, err))
			continue
		}
		e, err := parseSnapshot(result, started)
		if err == nil {
			out[key] = e
		} else if !errors.Is(err, layercache.ErrMiss) {
			errs = errors.Join(errs, fmt.Errorf("redisstore: get %q: %w", key, err))
		}
	}
	return out, errs
}
func (s *Store) MSet(ctx context.Context, values map[string][]byte, ttl time.Duration) (map[string]time.Time, error) {
	out := make(map[string]time.Time, len(values))
	if len(values) == 0 {
		return out, nil
	}
	ms := ttl / time.Millisecond
	if ms <= 0 {
		return out, layercache.ErrInvalidTTL
	}
	client, ok := s.client.(pipeliner)
	if !ok {
		var errs error
		for key, data := range values {
			deadline, err := s.Set(ctx, key, data, ttl)
			if err == nil {
				out[key] = deadline
			} else {
				errs = errors.Join(errs, err)
			}
		}
		return out, errs
	}
	pipe := client.Pipeline()
	commands := make(map[string]*redis.StatusCmd, len(values))
	deadline := time.Now().Add((ms - 1) * time.Millisecond)
	for key, data := range values {
		commands[key] = pipe.Set(ctx, key, data, ms*time.Millisecond)
	}
	_, errs := pipe.Exec(ctx)
	for key, cmd := range commands {
		if err := cmd.Err(); err == nil {
			out[key] = deadline
		} else {
			errs = errors.Join(errs, fmt.Errorf("redisstore: set %q: %w", key, err))
		}
	}
	return out, errs
}

var _ layercache.ConditionalRemote = (*Store)(nil)
var _ layercache.BatchRemote = (*Store)(nil)

const leaseScript = `local owner = redis.call('GET', KEYS[1])
if not owner or owner == ARGV[1] then
 redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
 return 1
end
return 0`

// AcquireLease allows the current owner to renew without waiting for expiry;
// otherwise request latency can make a healthy fixed-interval refresher skip ticks.
func (s *Store) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (time.Time, bool, error) {
	if owner == "" {
		return time.Time{}, false, layercache.ErrInvalidOptions
	}
	ms := ttl / time.Millisecond
	if ms <= 0 {
		return time.Time{}, false, layercache.ErrInvalidTTL
	}
	deadline := time.Now().Add((ms - 1) * time.Millisecond)
	value, err := s.eval(ctx, leaseScript, key, true, owner, int64(ms))
	if err != nil {
		return time.Time{}, false, err
	}
	n, ok := value.(int64)
	if !ok || (n != 0 && n != 1) {
		return time.Time{}, false, layercache.ErrCorrupt
	}
	if n == 0 {
		return time.Time{}, false, nil
	}
	return deadline, true, nil
}

var _ layercache.LeaseRemote = (*Store)(nil)

// Only commands whose boolean outcome can be changed by a retry use this
// wrapper. MOVED/ASK routing still works; ambiguous network failures surface to
// the coordinator so it fences old work instead of claiming a definite rejection.
type oneShotCmd struct{ *redis.Cmd }

func (*oneShotCmd) NoRetry() bool        { return true }
func (c *oneShotCmd) Clone() redis.Cmder { return &oneShotCmd{c.Cmd.Clone().(*redis.Cmd)} }

var _ redis.Cmder = (*oneShotCmd)(nil)

func (s *Store) process(ctx context.Context, cmd redis.Cmder) error {
	if err := s.client.Process(ctx, cmd); err != nil {
		return err
	}
	return cmd.Err()
}
func (s *Store) eval(ctx context.Context, script, key string, oneShot bool, args ...any) (any, error) {
	argv := append([]any{"eval", script, 1, key}, args...)
	cmd := redis.NewCmd(ctx, argv...)
	cmd.SetFirstKeyPos(3)
	var dispatched redis.Cmder = cmd
	if oneShot {
		dispatched = &oneShotCmd{cmd}
	}
	if err := s.process(ctx, dispatched); err != nil {
		return nil, err
	}
	return cmd.Result()
}
