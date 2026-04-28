package ratelimit

import (
	"context"
	"net/http"
	"time"

	common "github.com/platformcore/libpackage/plugins/common"
	ent "github.com/platformcore/libpackage/ratelimit/enterprise"
	legacy "github.com/platformcore/libpackage/ratelimit/legacy"
	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc"
)

const (
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

func DefaultPolicy() Policy {
	return Policy{
		Name:       "default",
		Limit:      100,
		Window:     time.Minute,
		Strategy:   StrategyFixedWindow,
		Cost:       1,
		ShadowMode: false,
	}
}

type EnterpriseAlgorithm = ent.Algorithm

const (
	EnterpriseAlgorithmTokenBucket   = ent.AlgorithmTokenBucket
	EnterpriseAlgorithmSlidingWindow = ent.AlgorithmSlidingWindow
	EnterpriseAlgorithmFixedWindow   = ent.AlgorithmFixedWindow
	EnterpriseAlgorithmLeakyBucket   = ent.AlgorithmLeakyBucket
)

type EnterpriseRateLimitResult = ent.RateLimitResult
type EnterpriseLimitConfig = ent.LimitConfig
type EnterpriseLimiter = ent.Limiter
type EnterpriseRedisLimiter = ent.RedisLimiter
type EnterpriseRedisLimiterConfig = ent.RedisLimiterConfig
type EnterpriseMemoryLimiter = ent.MemoryLimiter
type EnterpriseCompositeLimiter = ent.CompositeLimiter
type EnterpriseQuota = ent.Quota
type EnterpriseQuotaPeriod = ent.QuotaPeriod
type EnterpriseQuotaManager = ent.QuotaManager
type EnterpriseKeyExtractor = ent.KeyExtractor

const (
	EnterpriseQuotaPeriodHourly  = ent.QuotaPeriodHourly
	EnterpriseQuotaPeriodDaily   = ent.QuotaPeriodDaily
	EnterpriseQuotaPeriodWeekly  = ent.QuotaPeriodWeekly
	EnterpriseQuotaPeriodMonthly = ent.QuotaPeriodMonthly
)

func NewEnterpriseCompositeLimiter() *EnterpriseCompositeLimiter {
	return ent.NewCompositeLimiter()
}

func NewEnterpriseQuotaManager(client *redis.Client, logger common.Logger) *EnterpriseQuotaManager {
	return ent.NewQuotaManager(client, logger)
}

func EnterpriseByIP(r *http.Request) string {
	return ent.ByIP(r)
}

func EnterpriseByAPIKey(header string) EnterpriseKeyExtractor {
	return ent.ByAPIKey(header)
}

func EnterpriseByUser(userFromCtx func(r *http.Request) string) EnterpriseKeyExtractor {
	return ent.ByUser(userFromCtx)
}

func EnterpriseHTTPMiddleware(limiter EnterpriseLimiter, cfg EnterpriseLimitConfig, keyExtractor EnterpriseKeyExtractor) func(http.Handler) http.Handler {
	return ent.HTTPMiddleware(limiter, cfg, keyExtractor)
}

func EnterpriseTieredHTTPMiddleware(limiter EnterpriseLimiter, tierExtractor func(*http.Request) string, tiers map[string]EnterpriseLimitConfig, keyExtractor EnterpriseKeyExtractor) func(http.Handler) http.Handler {
	return ent.TieredHTTPMiddleware(limiter, tierExtractor, tiers, keyExtractor)
}

type LegacyStrategy = legacy.Strategy
type LegacyKey = legacy.Key
type LegacyPolicy = legacy.Policy
type LegacyDecision = legacy.Decision
type LegacyDecisionHook = legacy.DecisionHook
type LegacyStore = legacy.Store
type LegacyStoreRequest = legacy.StoreRequest
type LegacyStoreResponse = legacy.StoreResponse
type LegacyEvalStore = legacy.EvalStore
type LegacyOptions = legacy.Options
type LegacyEngine = legacy.Engine
type LegacyResult = legacy.Result
type LegacyLimiter = legacy.Limiter
type LegacyTokenBucketLimiter = legacy.TokenBucketLimiter
type LegacyFixedWindowLimiter = legacy.FixedWindowLimiter
type LegacySlidingWindowLimiter = legacy.SlidingWindowLimiter
type LegacyKeyExtractor = legacy.KeyExtractor
type LegacyMiddlewareConfig = legacy.MiddlewareConfig
type LegacyGRPCExtractor = legacy.GRPCExtractor
type LegacyRedisExecutor = legacy.RedisExecutor
type LegacyRedisStore = legacy.RedisStore
type LegacyMemoryStore = legacy.MemoryStore
type LegacyMultiLimiter = legacy.MultiLimiter

const (
	LegacyStrategyFixedWindow   = legacy.StrategyFixedWindow
	LegacyStrategySlidingWindow = legacy.StrategySlidingWindow
	LegacyStrategyTokenBucket   = legacy.StrategyTokenBucket
	LegacyHeaderLimit           = legacy.HeaderLimit
	LegacyHeaderRemaining       = legacy.HeaderRemaining
	LegacyHeaderReset           = legacy.HeaderReset
)

var (
	LegacyErrLimited       = legacy.ErrLimited
	LegacyErrNoStore       = legacy.ErrNoStore
	LegacyErrEmptyIdentity = legacy.ErrEmptyIdentity
	LegacyErrRedisExecutorNil = legacy.ErrRedisExecutorNil
)

func NewLegacyEngine(opts LegacyOptions) *LegacyEngine {
	return legacy.New(opts)
}

func NewLegacyKey(namespace, identity string) LegacyKey {
	return legacy.NewKey(namespace, identity)
}

func NewLegacyRedisStore(exec LegacyRedisExecutor, prefix string) *LegacyRedisStore {
	return legacy.NewRedisStore(exec, prefix)
}

func NewLegacyMemoryStore() *LegacyMemoryStore {
	return legacy.NewMemoryStore()
}

func NewLegacyMultiLimiter(limiters ...LegacyLimiter) *LegacyMultiLimiter {
	return legacy.NewMultiLimiter(limiters...)
}

func LegacyByIP() LegacyKeyExtractor {
	return legacy.ByIP()
}

func LegacyByHeader(header string) LegacyKeyExtractor {
	return legacy.ByHeader(header)
}

func LegacyByUser(ctxKey interface{}) LegacyKeyExtractor {
	return legacy.ByUser(ctxKey)
}

func LegacyByPath() LegacyKeyExtractor {
	return legacy.ByPath()
}

func LegacyCombine(extractors ...LegacyKeyExtractor) LegacyKeyExtractor {
	return legacy.Combine(extractors...)
}

func LegacyMiddleware(cfg LegacyMiddlewareConfig) func(http.Handler) http.Handler {
	return legacy.Middleware(cfg)
}

func LegacyHTTP(l *LegacyEngine, extract LegacyKeyExtractor) func(http.Handler) http.Handler {
	return legacy.HTTP(l, extract)
}

func LegacyUnaryServerInterceptor(l *LegacyEngine, extract LegacyGRPCExtractor) grpc.UnaryServerInterceptor {
	return legacy.UnaryServerInterceptor(l, extract)
}

func LegacyLuaScriptForStrategy(strategy LegacyStrategy) string {
	return legacy.LuaScriptForStrategy(strategy)
}

func NewDefaultUnified() *Unified {
	return NewUnified(UnifiedOptions{
		CorePolicy:            DefaultPolicy(),
		EnableEnterpriseMemory: true,
		LegacyTokenRate:       100.0 / 60.0,
		LegacyTokenBurst:      100,
		LegacyFixedLimit:      100,
		LegacyFixedWindow:     time.Minute,
		LegacySlidingLimit:    100,
		LegacySlidingWindow:   time.Minute,
	})
}

func MustAllow(ctx context.Context, l *Limiter, key Key) Result {
	return l.MustAllow(ctx, key)
}

