// Package redis_gen allocates numeric IDs through one atomic Redis operation
// per batch. All generators sharing server IDs must use the same Redis state.
// redis_gen 每批通过一次原子 Redis 操作分配数字 ID；共享 server ID 的所有
// 生成器必须使用同一份 Redis 状态。
package redis_gen

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/apus-run/gala/components/idgen"
	"github.com/redis/go-redis/v9"
)

// MaxBatchSize bounds allocation memory and work per call.
// MaxBatchSize 限制单次分配的内存与工作量。
const MaxBatchSize = 65536

// The legacy layout is seconds:32 | milliseconds:10 | sequence:8 | server:14.
// Reserve the sign bit so every returned int64 stays nonnegative.
// 旧布局为 秒:32 | 毫秒:10 | 序列:8 | 服务器:14。保留符号位，返回的 int64 恒非负。
const maxMillis int64 = (1<<31)*1000 - 1

// ErrStateMissing prevents silently recreating a lost allocation history.
// ErrStateMissing 防止静默重建丢失的分配历史。
var ErrStateMissing = errors.New("idgen: allocation state missing; explicit initialization required")

// Allocate an interval in a persistent (millisecond, sequence) space. Redis
// TIME supplies wall time; retained progress handles rollback and restarts.
// Never encode a full 64-bit ID in Lua, whose numbers cannot represent it.
// allocate 在持久的（毫秒，序列）空间里分配区间。墙钟来自 Redis TIME；
// 保留的进度可应对回拨与重启。绝不能在 Lua 里编码完整 64 位 ID——
// 其数值类型无法精确表示。
const allocate = `
local count = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local clock = redis.call('TIME')
local ms = tonumber(clock[1]) * 1000 + math.floor(tonumber(clock[2]) / 1000)
local seq = 0
local state = redis.call('HMGET', KEYS[1], 'ms', 'seq')
if not state[1] and not state[2] and redis.call('EXISTS', KEYS[1]) == 0 then
 return redis.error_reply('IDGEN_STATE_MISSING')
end
if state[1] or state[2] then
 local lastms, lastseq = tonumber(state[1]), tonumber(state[2])
 if not lastms or not lastseq or lastms < 0 or lastms > limit or lastms ~= math.floor(lastms)
    or lastseq < 0 or lastseq > 255 or lastseq ~= math.floor(lastseq) then
  return redis.error_reply('idgen: invalid allocation state')
 end
 if ms <= lastms then ms = lastms; seq = lastseq + 1 end
elseif redis.call('EXISTS', KEYS[1]) == 1 then
 return redis.error_reply('idgen: invalid allocation state')
end
if redis.call('PTTL', KEYS[1]) >= 0 then
 return redis.error_reply('idgen: allocation state must not expire')
end
ms = ms + math.floor(seq / 256)
seq = seq % 256
local last = seq + count - 1
local endms = ms + math.floor(last / 256)
if ms < 0 or endms > limit then return redis.error_reply('idgen: timestamp exhausted') end
redis.call('HSET', KEYS[1], 'ms', endms, 'seq', last % 256)
return {ms, seq}
`

// NewIDGenerator constructs a concurrent-safe generator. srvIDs are 14-bit
// partitions, not exclusive machine leases: different processes may share them.
// All producers in a numeric ID domain must share this Redis allocation state.
// Initialize partitions explicitly before generation; preserve their history.
// NewIDGenerator 构造并发安全的生成器。srvIDs 是 14 位分区而非独占的机器租约：
// 不同进程可共用。同一数字 ID 域的所有生产者必须共享这份 Redis 分配状态。
// 生成前请显式初始化分区，并保留其历史。
func NewIDGenerator(client *redis.Client, srvIDs []int64) (idgen.IDGenerator, error) {
	if client == nil {
		return nil, errors.New("idgen: Redis client is required")
	}
	if len(srvIDs) == 0 {
		return nil, errors.New("idgen: at least one server ID is required")
	}
	for _, n := range srvIDs {
		if n < 0 || n > 0x3fff {
			return nil, fmt.Errorf("idgen: invalid server ID %d", n)
		}
	}
	return &generator{cli: client, srvIDs: append([]int64(nil), srvIDs...)}, nil
}

type generator struct {
	cli    *redis.Client
	srvIDs []int64
}

// GenID allocates one numeric ID | GenID 分配一个数字 ID.
func (g *generator) GenID(ctx context.Context) (int64, error) {
	ids, err := g.GenMultiIDs(ctx, 1)
	if err != nil {
		return 0, fmt.Errorf("idgen: generate: %w", err)
	}
	return ids[0], nil
}

// GenMultiIDs allocates count consecutive numeric IDs | GenMultiIDs 批量分配 count 个连续数字 ID.
func (g *generator) GenMultiIDs(ctx context.Context, count int) ([]int64, error) {
	if count < 0 || count > MaxBatchSize {
		return nil, fmt.Errorf("idgen: count must be in [0, %d]", MaxBatchSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count == 0 {
		return []int64{}, nil
	}
	server := g.srvIDs[rand.IntN(len(g.srvIDs))]
	start, err := g.cli.Eval(ctx, allocate, []string{stateKey(server)}, count, maxMillis).Int64Slice()
	if err != nil {
		if strings.TrimPrefix(err.Error(), "ERR ") == "IDGEN_STATE_MISSING" {
			return nil, ErrStateMissing
		}
		return nil, fmt.Errorf("idgen: allocate: %w", err)
	}
	if len(start) != 2 || start[0] < 0 || start[0] > maxMillis || start[1] < 0 || start[1] > 255 {
		return nil, errors.New("idgen: invalid allocation response")
	}
	if start[0]+(start[1]+int64(count)-1)/256 > maxMillis {
		return nil, errors.New("idgen: allocation exceeds timestamp space")
	}
	ids := make([]int64, count)
	for n := range ids {
		offset := start[1] + int64(n)
		ms := start[0] + offset/256
		ids[n] = (ms/1000)<<32 | (ms%1000)<<22 | (offset%256)<<14 | server
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func stateKey(server int64) string {
	return fmt.Sprintf("id_generator:v2:%d", server)
}

const initialize = `
if redis.call('EXISTS', KEYS[1]) == 1 then return redis.error_reply('idgen: state already exists') end
redis.call('HSET', KEYS[1], 'ms', ARGV[1], 'seq', ARGV[2])
return 1
`

// InitializeState explicitly creates a missing Redis partition without replacing
// existing state. Use (0,0) only for a never-used numeric partition. Recovery
// requires a verified last allocated position; a lower seed can repeat IDs.
// InitializeState 显式创建缺失的 Redis 分区，不替换已有状态。(0,0) 仅用于从未
// 使用过的数字分区。恢复需要经过核实的最后分配位置；种子偏小可能重复发号。
func InitializeState(ctx context.Context, client *redis.Client, serverID, lastMillis, lastSequence int64) error {
	if client == nil || serverID < 0 || serverID > 16383 || lastMillis < 0 || lastMillis > maxMillis || lastSequence < 0 || lastSequence > 255 {
		return fmt.Errorf("idgen: invalid Redis state initialization")
	}
	_, err := client.Eval(ctx, initialize, []string{stateKey(serverID)}, lastMillis, lastSequence).Result()
	return err
}
