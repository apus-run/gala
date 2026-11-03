package layercache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"
)

func (c *Cache) admitRead(ctx context.Context, t *ticket, skip bool) (Entry, bool, error) {
	if err := c.lockKey(ctx, t); err != nil {
		return Entry{}, false, err
	}
	defer unlockKey(t)
	if err := c.checkGeneration(t); err != nil {
		return Entry{}, false, err
	}
	if skip {
		return Entry{}, false, nil
	}
	e, hit := c.getLocal(t.key)
	return e, hit, nil
}
func (c *Cache) readCache(ctx context.Context, t *ticket, skip bool) (Entry, error) {
	e, hit, err := c.admitRead(ctx, t, skip)
	if err != nil || hit {
		return e, err
	}
	err = ErrMiss
	if c.remote != nil {
		op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
		defer cancel()
		e, err = c.remote.Get(op, t.key)
		if x := c.contextError(op); x != nil {
			err = x
		}
	}
	return c.acceptRemote(ctx, t, e, err, skip)
}
func (c *Cache) acceptRemote(ctx context.Context, t *ticket, e Entry, err error, skip bool) (Entry, error) {
	if err == nil {
		err = validateSnapshot(e)
	}
	if c.remote != nil {
		if err == nil {
			c.incr(statRemoteHit)
		} else if errors.Is(err, ErrMiss) {
			c.incr(statRemoteMiss)
		}
	}
	if x := c.lockKey(ctx, t); x != nil {
		return Entry{}, x
	}
	defer unlockKey(t)
	if x := c.checkGeneration(t); x != nil {
		return Entry{}, x
	}
	if err != nil {
		return Entry{}, &OpError{"get", t.key, err}
	}
	if !time.Now().Before(e.ExpiresAt) {
		return Entry{}, ErrMiss
	}
	if !skip {
		c.setLocal(t.key, e, e.ExpiresAt)
	}
	return e, nil
}
func validateSnapshot(e Entry) error {
	if e.ExpiresAt.IsZero() {
		return ErrInvalidTTL
	}
	if !time.Now().Before(e.ExpiresAt) {
		return ErrMiss
	}
	_, _, err := envelope(e.Data)
	return err
}
func (c *Cache) ttl(ttl time.Duration, negative bool) time.Duration {
	if negative {
		ttl = c.opts.notFoundTTL
	}
	if c.opts.jitter > 0 {
		ttl += time.Duration(rand.Int64N(int64(c.opts.jitter)))
	}
	return ttl
}

// commitEntry publishes one mutation while the caller holds the key gate.
// Its boolean reports a confirmed mutation, independently of a later timeout.
// Rejected conditions have no local/refresh side effects. A write with unknown
// outcome still fences older snapshots. Rebase only this operation's ticket;
// other operations keep their old generation and fail validation.
// commitEntry 在调用者持有 key 闸门时发布一次变更。布尔值报告已确认的变更，
// 与之后的超时无关。被拒绝的条件写不产生本地/刷新副作用。结果未知的写入
// 仍会栅栏化更旧的快照。只对本操作的 ticket 重定代；其他操作保持旧 generation
// 并在校验时失败。
func (c *Cache) commitEntry(ctx context.Context, t *ticket, data []byte, i ItemOptions, stopRefresh bool) (bool, error) {
	if c.remote == nil && i.mode() != SetAlways {
		_, exists := c.getLocal(t.key)
		if i.nx && exists || i.xx && !exists {
			return false, ErrNotStored
		}
	}
	var deadline time.Time
	var err error
	conditional := c.remote != nil && i.mode() != SetAlways
	if conditional {
		deadline, err = c.writeRemote(ctx, t.key, data, c.ttl(i.ttl, isNegative(data)), i.mode())
		if errors.Is(err, ErrNotStored) {
			return false, err
		}
	}
	c.invalidate(t, stopRefresh)
	c.removeLocal(t.key)
	if conditional {
		committed := !deadline.IsZero()
		if err != nil {
			return committed, err
		}
		if err := c.contextError(ctx); err != nil {
			return committed, err
		}
		if !i.skipLocal {
			c.setLocal(t.key, Entry{Data: data}, deadline)
		}
		return true, nil
	}
	return c.writeEntry(ctx, t.key, data, i)
}

// All writes, including refresh, share this backend-first commit path.
// 包括刷新在内的所有写入都走这条"后端优先"的提交路径。
func (c *Cache) writeEntry(ctx context.Context, key string, data []byte, i ItemOptions) (bool, error) {
	ttl := c.ttl(i.ttl, isNegative(data))
	if ttl <= 0 {
		return false, ErrInvalidTTL
	}
	deadline, err := c.writeRemote(ctx, key, data, ttl, i.mode())
	committed := c.remote != nil && !deadline.IsZero()
	if err != nil {
		return committed, err
	}
	if err := c.contextError(ctx); err != nil {
		return committed, err
	}
	if !i.skipLocal {
		c.setLocal(key, Entry{Data: data}, deadline)
	}
	return true, nil
}
func (c *Cache) writeRemote(ctx context.Context, key string, data []byte, ttl time.Duration, mode SetMode) (time.Time, error) {
	limit := time.Now().Add(ttl)
	if c.remote == nil {
		return limit, nil
	}
	op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
	defer cancel()
	var deadline time.Time
	var err error
	if mode == SetAlways {
		deadline, err = c.remote.Set(op, key, data, ttl)
	} else {
		r, ok := c.remote.(ConditionalRemote)
		if !ok {
			return time.Time{}, ErrUnsupported
		}
		var stored bool
		deadline, stored, err = r.SetIf(op, key, data, ttl, mode)
		if err == nil && !stored {
			err = ErrNotStored
		}
	}
	if err == nil && deadline.IsZero() {
		err = ErrInvalidTTL
	}
	if err != nil {
		deadline = time.Time{}
	}
	if deadline.After(limit) {
		deadline = limit
	}
	if x := c.contextError(op); x != nil {
		// A received rejection is a known no-write outcome even if cancellation
		// races with the reply. Keep both facts so it cannot cancel refresh.
		// 即使取消与应答竞速，已收到的拒绝也是确定的"未写入"结果。
		// 同时保留两个事实，避免它取消刷新。
		if errors.Is(err, ErrNotStored) {
			err = errors.Join(err, x)
		} else {
			err = x
		}
	}
	if err != nil {
		// A valid receipt survives a later timeout so callers can notify peers;
		// the error still prevents publishing a late local value.
		// 有效回执在后续超时后仍然保留，以便调用方通知对端；
		// 错误依旧会阻止发布迟到的本地值。
		return deadline, &OpError{"set", key, err}
	}
	return deadline, nil
}
func (c *Cache) getLocal(key string) (Entry, bool) {
	if c.local == nil {
		return Entry{}, false
	}
	e, ok := c.local.Get(key)
	ok = ok && !e.ExpiresAt.IsZero() && time.Now().Before(e.ExpiresAt)
	if ok {
		c.incr(statLocalHit)
	} else {
		c.incr(statLocalMiss)
	}
	return e, ok
}
func (c *Cache) removeLocal(key string) {
	if c.local != nil {
		c.local.Remove(key)
	}
}
func (c *Cache) setLocal(key string, e Entry, deadline time.Time) {
	if c.local == nil {
		return
	}
	c.removeLocal(key)
	if localDeadline := time.Now().Add(c.opts.localTTL); localDeadline.Before(deadline) {
		deadline = localDeadline
	}
	e.ExpiresAt = deadline
	if ttl := time.Until(deadline); ttl > 0 {
		c.local.Set(key, e, ttl)
	}
}

// Version 2 preserves JetCache's string/byte fast paths without confusing "*"
// with absence. Kind: 0 codec, 1 negative, 2 string, 3 bytes, 4 nil.
// Version 1 codec/negative records remain readable during migration.
// 版本 2 保留 JetCache 的 string/byte 快路径，且不会把 "*" 与空值混淆。
// Kind：0 编解码器，1 空值，2 string，3 bytes，4 nil。
// 迁移期仍可读版本 1 的 codec/negative 记录。
func (c *Cache) encode(value any, negative bool) ([]byte, error) {
	if negative {
		return []byte{'L', 'C', 2, 1}, nil
	}
	var payload []byte
	var err error
	kind := byte(0)
	switch v := value.(type) {
	case nil:
		kind = 4
	case string:
		kind = 2
		payload = []byte(v)
	case []byte:
		kind = 3
		payload = v
	default:
		payload, err = c.opts.codec.Marshal(value)
		if err != nil {
			return nil, err
		}
		if len(payload) == 0 {
			return nil, errors.New("layercache: codec returned an empty payload")
		}
	}
	return append([]byte{'L', 'C', 2, kind}, payload...), nil
}
func envelope(data []byte) (byte, []byte, error) {
	if len(data) < 4 || !bytes.Equal(data[:2], []byte("LC")) || (data[2] != 1 && data[2] != 2) {
		return 0, nil, ErrCorrupt
	}
	kind := data[3]
	payload := data[4:]
	if kind > 4 || (data[2] == 1 && kind > 1) || (kind == 0 && len(payload) == 0) || ((kind == 1 || kind == 4) && len(payload) != 0) {
		return 0, nil, ErrCorrupt
	}
	return kind, payload, nil
}
func isNegative(data []byte) bool { kind, _, err := envelope(data); return err == nil && kind == 1 }
func destination(dst any) error {
	if dst == nil {
		return nil
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("%w: destination must be a non-nil pointer", ErrInvalidOptions)
	}
	return nil
}
func (c *Cache) decode(data []byte, dst any) error {
	kind, payload, err := envelope(data)
	if err != nil {
		return err
	}
	if kind == 1 {
		return c.notFound()
	}
	if err := destination(dst); err != nil {
		return err
	}
	if dst == nil {
		return nil
	}
	if kind == 4 {
		reflect.ValueOf(dst).Elem().SetZero()
		return nil
	}
	// As in JetCache, raw destinations expose the serialized payload, and raw
	// inputs can be decoded into objects. The envelope is never exposed.
	// 与 JetCache 相同：裸目标暴露序列化后的负载，裸输入也能解码进对象；
	// 信封本身永不暴露。
	switch out := dst.(type) {
	case *string:
		*out = string(payload)
		return nil
	case *[]byte:
		*out = bytes.Clone(payload)
		return nil
	case *any:
		if kind == 2 {
			*out = string(payload)
			return nil
		}
		if kind == 3 {
			*out = bytes.Clone(payload)
			return nil
		}
	}
	if err := c.opts.codec.Unmarshal(payload, dst); err != nil {
		return errors.Join(ErrCorrupt, err)
	}
	return nil
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }
func (jsonCodec) Unmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func (c *Cache) defaultTTL() time.Duration {
	if c.remote == nil {
		return c.opts.localTTL
	}
	return c.opts.remoteTTL
}

func (c *Cache) deleteRemote(ctx context.Context, key string) (bool, error) {
	if c.remote == nil {
		return true, nil
	}
	op, cancel := context.WithTimeout(ctx, c.opts.remoteTimeout)
	defer cancel()
	err := c.remote.Delete(op, key)
	committed := err == nil
	if x := c.contextError(op); x != nil {
		err = x
	}
	if err != nil {
		return committed, &OpError{"delete", key, err}
	}
	return true, nil
}
