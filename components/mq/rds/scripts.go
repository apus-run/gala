package rds

// Each delayed member includes a unique ID, so identical payloads do not
// overwrite each other. Redis time avoids producer/scheduler clock disagreement.
const schedule = `
local now = redis.call('TIME')
local at = tonumber(now[1])*1000 + math.floor(tonumber(now[2])/1000) + tonumber(ARGV[2])
return redis.call('ZADD', KEYS[1], at, ARGV[1])`

// XADD precedes ZREM. If destination writes fail, the delayed item remains.
// Lua serializes competing schedulers; no separate read/remove race exists.
const promote = `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind ~= 'none' and kind ~= 'stream' then return redis.error_reply('redisstream: invalid destination stream') end
local now = redis.call('TIME')
local ms = tonumber(now[1])*1000 + math.floor(tonumber(now[2])/1000)
local items = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ms, 'LIMIT', 0, 128)
for _, data in ipairs(items) do
 redis.call('XADD', KEYS[1], '*', 'data', data)
 redis.call('ZREM', KEYS[2], data)
end
return #items`

// Verify both owner and delivery count: a slow/stale handler cannot ACK or
// dead-letter a delivery reclaimed by another worker (or a later attempt).
// A repeated invocation after an uncertain reply sees no pending entry.
const finish = `
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[2], ARGV[2], 1)
if #p == 0 or p[1][2] ~= ARGV[3] or p[1][4] ~= tonumber(ARGV[4]) then return 0 end
if ARGV[5] == 'dead' then
 local kind = redis.call('TYPE', KEYS[2]).ok
 if kind ~= 'none' and kind ~= 'stream' then return redis.error_reply('redisstream: invalid dead-letter stream') end
 local original = redis.call('XRANGE', KEYS[1], ARGV[2], ARGV[2])
 if #original == 0 then return redis.error_reply('redisstream: pending message was deleted') end
 local fields = original[1][2]
 table.insert(fields, '_source_id'); table.insert(fields, ARGV[2])
 table.insert(fields, '_attempts'); table.insert(fields, ARGV[4])
 table.insert(fields, '_error'); table.insert(fields, ARGV[6])
 redis.call('XADD', KEYS[2], '*', unpack(fields))
end
return redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])`

// Compare unsigned 64-bit ID parts as decimal strings, avoiding Lua's floating
// point precision limit. All reads precede writes in this atomic operation.
const trim = `
local function partLess(a, b)
 if #a ~= #b then return #a < #b end
 return a < b
end
local function less(a, b)
 local am, as = string.match(a, '^(%d+)%-(%d+)$')
 local bm, bs = string.match(b, '^(%d+)%-(%d+)$')
 if am ~= bm then return partLess(am, bm) end
 return partLess(as, bs)
end
local groups = redis.call('XINFO', 'GROUPS', KEYS[1])
local floor = nil
local inclusive = false
for _, fields in ipairs(groups) do
 local group = {}
 for i = 1, #fields, 2 do group[fields[i]] = fields[i+1] end
 local bound = group['last-delivered-id']
 if bound == '0' then bound = '0-0' end
 local include = true
 local pending = redis.call('XPENDING', KEYS[1], group.name)
 if pending[1] > 0 and not less(bound, pending[2]) then
  bound = pending[2]
  include = false
 end
 if not floor or less(bound, floor) then
  floor = bound
  inclusive = include
 elseif bound == floor then
  inclusive = inclusive and include
 end
end
if not floor then return 0 end
local removed = redis.call('XTRIM', KEYS[1], 'MINID', floor)
if inclusive then removed = removed + redis.call('XDEL', KEYS[1], floor) end
return removed`
