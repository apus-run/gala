-- Extend the lock only when it is still owned by this token.
-- KEYS[1] lock key
-- ARGV[1] owner token
-- ARGV[2] TTL in milliseconds
if redis.call('get', KEYS[1]) == ARGV[1] then
    return redis.call('pexpire', KEYS[1], ARGV[2])
end
return 0
