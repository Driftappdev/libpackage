package goratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

type RedisExecutor interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) (any, error)
}

const RedisFixedWindowLua = `
local current = redis.call("INCR", KEYS[1])
if current == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
return {current, ttl}
`

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

type RedisStore struct {
	exec   RedisExecutor
	prefix string
}

func NewRedisStore(exec RedisExecutor, prefix string) *RedisStore {
	if prefix == "" {
		prefix = "rl"
	}
	return &RedisStore{exec: exec, prefix: prefix}
}

func (s *RedisStore) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + ":" + k
}

func (s *RedisStore) Increment(ctx context.Context, key string, window time.Duration, now time.Time) (int, time.Time, error) {
	res, err := s.Eval(ctx, StoreRequest{
		Key: key,
		Policy: Policy{
			Name:     "redis-fixed-window",
			Limit:    1 << 30,
			Window:   window,
			Strategy: StrategyFixedWindow,
			Cost:     1,
		},
		Now: now,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	return res.Used, res.ResetAt, nil
}

func (s *RedisStore) Eval(ctx context.Context, req StoreRequest) (StoreResponse, error) {
	if s.exec == nil {
		return StoreResponse{}, ErrRedisExecutorNil
	}
	p := req.Policy.Normalize()
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	key := s.key(req.Key)
	script := LuaScriptForStrategy(p.Strategy)

	switch p.Strategy {
	case StrategySlidingWindow:
		raw, err := s.exec.Eval(ctx, script, []string{key}, now.UnixMilli(), p.Window.Milliseconds(), p.Cost)
		if err != nil {
			return StoreResponse{}, err
		}
		used, ttlMs, err := parseTwoIntReply(raw)
		if err != nil {
			return StoreResponse{}, err
		}
		resetAt := now.Add(time.Duration(ttlMs) * time.Millisecond)
		return StoreResponse{
			Used:      used,
			Limit:     p.Limit,
			Remaining: maxInt(0, p.Limit-used),
			ResetAt:   resetAt,
			Allowed:   used <= p.Limit,
		}, nil
	case StrategyTokenBucket:
		raw, err := s.exec.Eval(ctx, script, []string{key}, p.Burst, p.RefillRatePerSecond, now.UnixMilli(), p.Cost)
		if err != nil {
			return StoreResponse{}, err
		}
		allowed, tokens, ttlMs, err := parseTokenBucketReply(raw)
		if err != nil {
			return StoreResponse{}, err
		}
		remaining := int(tokens)
		if remaining < 0 {
			remaining = 0
		}
		return StoreResponse{
			Used:      p.Burst - remaining,
			Limit:     p.Burst,
			Remaining: remaining,
			ResetAt:   now.Add(time.Duration(ttlMs) * time.Millisecond),
			Allowed:   allowed,
		}, nil
	default:
		raw, err := s.exec.Eval(ctx, script, []string{key}, p.Window.Milliseconds())
		if err != nil {
			return StoreResponse{}, err
		}
		used, ttlMs, err := parseTwoIntReply(raw)
		if err != nil {
			return StoreResponse{}, err
		}
		resetAt := now.Add(time.Duration(ttlMs) * time.Millisecond)
		return StoreResponse{
			Used:      used,
			Limit:     p.Limit,
			Remaining: maxInt(0, p.Limit-used),
			ResetAt:   resetAt,
			Allowed:   used <= p.Limit,
		}, nil
	}
}

func parseTwoIntReply(raw any) (int, int64, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 2 {
		return 0, 0, fmt.Errorf("unexpected redis reply: %T", raw)
	}
	a, err := toInt(arr[0])
	if err != nil {
		return 0, 0, err
	}
	b, err := toInt64(arr[1])
	if err != nil {
		return 0, 0, err
	}
	return a, b, nil
}

func parseTokenBucketReply(raw any) (bool, float64, int64, error) {
	arr, ok := raw.([]any)
	if !ok || len(arr) < 3 {
		return false, 0, 0, fmt.Errorf("unexpected token bucket reply: %T", raw)
	}
	allowedInt, err := toInt64(arr[0])
	if err != nil {
		return false, 0, 0, err
	}
	tokens, err := toFloat64(arr[1])
	if err != nil {
		return false, 0, 0, err
	}
	ttl, err := toInt64(arr[2])
	if err != nil {
		return false, 0, 0, err
	}
	return allowedInt == 1, tokens, ttl, nil
}

func toInt(v any) (int, error) {
	i64, err := toInt64(v)
	if err != nil {
		return 0, err
	}
	return int(i64), nil
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case uint64:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case string:
		return strconv.ParseInt(x, 10, 64)
	case []byte:
		return strconv.ParseInt(string(x), 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to int64", v)
	}
}

func toFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case int:
		return float64(x), nil
	case string:
		return strconv.ParseFloat(x, 64)
	case []byte:
		return strconv.ParseFloat(string(x), 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
