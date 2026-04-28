package goratelimit

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrLimited         = errors.New("rate limited")
	ErrNoStore         = errors.New("store is nil")
	ErrEmptyIdentity   = errors.New("empty key identity")
	ErrRedisExecutorNil = errors.New("redis executor is nil")
)

type Strategy string

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
	StrategyTokenBucket   Strategy = "token_bucket"
)

const (
	HeaderLimit     = "X-RateLimit-Limit"
	HeaderRemaining = "X-RateLimit-Remaining"
	HeaderReset     = "X-RateLimit-Reset"
)

type Key struct {
	Namespace  string
	Identity   string
	Route      string
	Method     string
	Tenant     string
	Dimensions map[string]string
}

func NewKey(namespace, identity string) Key {
	return Key{Namespace: namespace, Identity: identity, Dimensions: map[string]string{}}
}

func (k Key) Validate() error {
	if strings.TrimSpace(k.Identity) == "" {
		return ErrEmptyIdentity
	}
	return nil
}

func (k Key) String() string {
	identity := normalizePart(k.Identity)
	namespace := normalizePart(k.Namespace)
	if namespace != "" {
		identity = namespace + ":" + identity
	}
	parts := make([]string, 0, 4+len(k.Dimensions))
	if r := normalizePart(k.Route); r != "" {
		parts = append(parts, "route="+r)
	}
	if m := strings.ToUpper(strings.TrimSpace(k.Method)); m != "" {
		parts = append(parts, "method="+m)
	}
	if t := normalizePart(k.Tenant); t != "" {
		parts = append(parts, "tenant="+t)
	}
	if len(k.Dimensions) > 0 {
		keys := make([]string, 0, len(k.Dimensions))
		for n := range k.Dimensions {
			keys = append(keys, n)
		}
		sort.Strings(keys)
		for _, n := range keys {
			parts = append(parts, normalizePart(n)+"="+normalizePart(k.Dimensions[n]))
		}
	}
	if len(parts) == 0 {
		return identity
	}
	return identity + "|" + strings.Join(parts, "|")
}

func normalizePart(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.ReplaceAll(v, " ", "_")
	return v
}

type Policy struct {
	Name                string
	Limit               int
	Window              time.Duration
	Strategy            Strategy
	Cost                int
	Burst               int
	RefillRatePerSecond float64
	ShadowMode          bool
}

func (p Policy) Normalize() Policy {
	if p.Name == "" {
		p.Name = "default"
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Window <= 0 {
		p.Window = time.Minute
	}
	if p.Strategy == "" {
		p.Strategy = StrategyFixedWindow
	}
	if p.Cost <= 0 {
		p.Cost = 1
	}
	if p.Burst <= 0 {
		p.Burst = p.Limit
	}
	if p.RefillRatePerSecond <= 0 {
		p.RefillRatePerSecond = float64(p.Limit) / p.Window.Seconds()
	}
	return p
}

type Decision struct {
	Key       string
	Result    Result
	Err       error
	Timestamp time.Time
}

type DecisionHook func(Decision)

type Store interface {
	Increment(ctx context.Context, key string, window time.Duration, now time.Time) (count int, resetAt time.Time, err error)
}

type StoreRequest struct {
	Key    string
	Policy Policy
	Now    time.Time
}

type StoreResponse struct {
	Used      int
	Limit     int
	Remaining int
	ResetAt   time.Time
	Allowed   bool
	Metadata  map[string]string
}

type EvalStore interface {
	Eval(ctx context.Context, req StoreRequest) (StoreResponse, error)
}

type Options struct {
	Policy   Policy
	Store    Store
	Now      func() time.Time
	FailOpen bool
	OnResult DecisionHook
}

func (o Options) normalize() Options {
	o.Policy = o.Policy.Normalize()
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

func (r Result) Headers() http.Header {
	h := make(http.Header)
	h.Set(HeaderLimit, strconv.Itoa(r.Limit))
	h.Set(HeaderRemaining, strconv.Itoa(r.Remaining))
	if r.ResetAfter > 0 {
		h.Set(HeaderReset, strconv.FormatInt(time.Now().Add(r.ResetAfter).Unix(), 10))
	}
	if r.RetryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(int(r.RetryAfter.Seconds())))
	}
	return h
}
