-- Release the lock only when it is still owned by this token.
-- KEYS[1] lock key
-- ARGV[1] owner token
if redis.call('get', KEYS[1]) == ARGV[1] then
    return redis.call('del', KEYS[1])
end
return 0
