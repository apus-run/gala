-- Acquire the lock, or extend it when this owner already holds it.
-- KEYS[1] lock key
-- ARGV[1] owner token
-- ARGV[2] TTL in milliseconds
local current = redis.call('get', KEYS[1])
if current == false then
    redis.call('set', KEYS[1], ARGV[1], 'PX', ARGV[2])
    return 'OK'
end
if current == ARGV[1] then
    redis.call('pexpire', KEYS[1], ARGV[2])
    return 'OK'
end
return ''
