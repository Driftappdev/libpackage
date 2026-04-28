package ratelimit

import (
	"time"

	legacy "github.com/platformcore/libpackage/ratelimit/legacy"
	ent "github.com/platformcore/libpackage/ratelimit/enterprise"
	base "github.com/platformcore/libpackage/ratelimit/limiter"
)

// Unified exposes one entrypoint for core, enterprise, and legacy limiter stacks.
// This is the single package surface for services that need all capabilities.
type Unified struct {
	Core       *base.Limiter
	Enterprise *EnterpriseSuite
	Legacy     *LegacySuite
}

// EnterpriseSuite groups enterprise-grade limiters and middleware.
type EnterpriseSuite struct {
	Redis  *ent.RedisLimiter
	Memory *ent.MemoryLimiter
}

// LegacySuite groups compatibility limiters from goratelimit/ratelimit1 lineage.
type LegacySuite struct {
	Engine       *legacy.Engine
	TokenBucket  *legacy.TokenBucketLimiter
	FixedWindow  *legacy.FixedWindowLimiter
	SlidingWindow *legacy.SlidingWindowLimiter
}

// UnifiedOptions configures which stacks should be bootstrapped.
type UnifiedOptions struct {
	CorePolicy base.Policy
	CoreStore  base.Store
	CoreNow    func() time.Time
	CoreFailOpen bool
	CoreHook   base.DecisionHook

	EnterpriseRedisConfig *ent.RedisLimiterConfig
	EnableEnterpriseMemory bool

	LegacyOptions      *legacy.Options
	LegacyTokenRate    float64
	LegacyTokenBurst   int
	LegacyFixedLimit   int
	LegacyFixedWindow  time.Duration
	LegacySlidingLimit int
	LegacySlidingWindow time.Duration
}

// NewUnified builds a single ratelimit bundle for all major feature sets.
func NewUnified(opts UnifiedOptions) *Unified {
	u := &Unified{
		Core: New(base.Options{
			Policy:   opts.CorePolicy,
			Store:    opts.CoreStore,
			Now:      opts.CoreNow,
			FailOpen: opts.CoreFailOpen,
			OnResult: opts.CoreHook,
		}),
		Enterprise: &EnterpriseSuite{},
		Legacy:     &LegacySuite{},
	}

	if opts.EnterpriseRedisConfig != nil {
		u.Enterprise.Redis = ent.NewRedisLimiter(*opts.EnterpriseRedisConfig)
	}
	if opts.EnableEnterpriseMemory {
		u.Enterprise.Memory = ent.NewMemoryLimiter()
	}

	if opts.LegacyOptions != nil {
		u.Legacy.Engine = legacy.New(*opts.LegacyOptions)
	}
	if opts.LegacyTokenRate > 0 && opts.LegacyTokenBurst > 0 {
		u.Legacy.TokenBucket = legacy.NewTokenBucket(opts.LegacyTokenRate, opts.LegacyTokenBurst)
	}
	if opts.LegacyFixedLimit > 0 && opts.LegacyFixedWindow > 0 {
		u.Legacy.FixedWindow = legacy.NewFixedWindow(opts.LegacyFixedLimit, opts.LegacyFixedWindow)
	}
	if opts.LegacySlidingLimit > 0 && opts.LegacySlidingWindow > 0 {
		u.Legacy.SlidingWindow = legacy.NewSlidingWindow(opts.LegacySlidingLimit, opts.LegacySlidingWindow)
	}

	return u
}

// NewEnterpriseRedisLimiter provides direct enterprise constructor via ratelimit package.
func NewEnterpriseRedisLimiter(cfg ent.RedisLimiterConfig) *ent.RedisLimiter {
	return ent.NewRedisLimiter(cfg)
}

// NewEnterpriseMemoryLimiter provides direct enterprise constructor via ratelimit package.
func NewEnterpriseMemoryLimiter() *ent.MemoryLimiter {
	return ent.NewMemoryLimiter()
}

// NewLegacyTokenBucket provides compatibility constructor via ratelimit package.
func NewLegacyTokenBucket(ratePerSec float64, burst int) *legacy.TokenBucketLimiter {
	return legacy.NewTokenBucket(ratePerSec, burst)
}

// NewLegacyFixedWindow provides compatibility constructor via ratelimit package.
func NewLegacyFixedWindow(limit int, window time.Duration) *legacy.FixedWindowLimiter {
	return legacy.NewFixedWindow(limit, window)
}

// NewLegacySlidingWindow provides compatibility constructor via ratelimit package.
func NewLegacySlidingWindow(limit int, window time.Duration) *legacy.SlidingWindowLimiter {
	return legacy.NewSlidingWindow(limit, window)
}

