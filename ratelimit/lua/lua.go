package ratelimit

type Strategy string

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

const RedisFixedWindowLua = `
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
return {current, ttl}
`

// Sliding-window counter approximation with two adjacent windows.
const RedisSlidingWindowLua = `
local now_ms = tonumber(ARGV[1])
local win_ms = tonumber(ARGV[2])
local cost = tonumber(ARGV[3])
local bucket = math.floor(now_ms / win_ms)
local prev_bucket = bucket - 1
local cur_key = KEYS[1] .. ":" .. bucket
local prev_key = KEYS[1] .. ":" .. prev_bucket

local cur = redis.call("INCRBY", cur_key, cost)
if cur == cost then
  redis.call("PEXPIRE", cur_key, win_ms * 2)
end
local prev = tonumber(redis.call("GET", prev_key) or "0")
local elapsed = now_ms - (bucket * win_ms)
local weight = (win_ms - elapsed) / win_ms
if weight < 0 then weight = 0 end
local estimated = cur + (prev * weight)
local ttl = redis.call("PTTL", cur_key)
return {estimated, ttl}
`

// Token-bucket implementation.
const RedisTokenBucketLua = `
local capacity = tonumber(ARGV[1])
local refill_per_sec = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])
local state = redis.call("HMGET", KEYS[1], "tokens", "ts")
local tokens = tonumber(state[1])
local ts = tonumber(state[2])

if tokens == nil then tokens = capacity end
if ts == nil then ts = now_ms end

local delta_sec = (now_ms - ts) / 1000.0
if delta_sec < 0 then delta_sec = 0 end
tokens = math.min(capacity, tokens + (delta_sec * refill_per_sec))

local allowed = 0
if tokens >= cost then
  tokens = tokens - cost
  allowed = 1
end

redis.call("HMSET", KEYS[1], "tokens", tokens, "ts", now_ms)
local ttl = math.ceil((capacity / refill_per_sec) * 1000)
if ttl < 1000 then ttl = 1000 end
redis.call("PEXPIRE", KEYS[1], ttl)
return {allowed, tokens, ttl}
`

func LuaScriptForStrategy(strategy Strategy) string {
	switch strategy {
	case StrategySlidingWindow:
		return RedisSlidingWindowLua
	case StrategyTokenBucket:
		return RedisTokenBucketLua
	default:
		return RedisFixedWindowLua
	}
}
