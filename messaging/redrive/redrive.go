package redrive

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

type Priority uint8

const (
	PriorityCritical Priority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
)

type Message struct {
	ID          string
	SenderID    string
	Topic       string
	Priority    Priority
	Body        []byte
	Metadata    map[string]string
	Timestamp   time.Time
	Attempts    int
	MaxAttempts int
}

type FailureRecord struct {
	AttemptedAt time.Time
	Error       string
	Attempt     int
}

type DeadMessage struct {
	Message    *Message
	Failures   []FailureRecord
	ArrivedAt  time.Time
	ExpiresAt  time.Time
	PoisonPill bool
	Replayed   bool
	Tags       map[string]string
}

func (dm *DeadMessage) LastError() string {
	if len(dm.Failures) == 0 {
		return ""
	}
	return dm.Failures[len(dm.Failures)-1].Error
}

type DLQFilter struct {
	Topic       string
	PoisonOnly  bool
	Before      time.Time
	After       time.Time
	ExpiredOnly bool
	Limit       int
	Offset      int
}

type Source interface {
	List(ctx context.Context, f DLQFilter) ([]*DeadMessage, error)
	Delete(ctx context.Context, id string) error
}

type Target interface {
	Submit(msg *Message) error
}

type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}

type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64
	lastFill time.Time
}

func newTokenBucket(capacity float64, ratePerSec float64) *tokenBucket {
	return &tokenBucket{
		tokens:   capacity,
		capacity: capacity,
		rate:     ratePerSec,
		lastFill: time.Now(),
	}
}

func (tb *tokenBucket) Allow(n float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastFill).Seconds()
	tb.tokens = math.Min(tb.capacity, tb.tokens+elapsed*tb.rate)
	tb.lastFill = now
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}

var (
	ErrRedriveAlreadyRunning = errors.New("redrive: an operation is already in progress")
	ErrRedriveCancelled      = errors.New("redrive: cancelled by caller")
	ErrRedriveNoTarget       = errors.New("redrive: no target")
	ErrRedriveNoSource       = errors.New("redrive: no source")
	ErrRedriveInvalidFilter  = errors.New("redrive: invalid filter")
)

type RedriveFilter struct {
	Topics        []string
	SenderIDs     []string
	MinAttempts   int
	MaxAttempts   int
	OlderThan     time.Duration
	NewerThan     time.Duration
	ErrorPattern  string
	Tags          map[string]string
	ExcludePoison bool
	Limit         int
	errorRe       *regexp.Regexp
}

func (f *RedriveFilter) compile() error {
	if f.ErrorPattern != "" {
		re, err := regexp.Compile(f.ErrorPattern)
		if err != nil {
			return fmt.Errorf("%w: error pattern: %v", ErrRedriveInvalidFilter, err)
		}
		f.errorRe = re
	}
	return nil
}

func (f *RedriveFilter) matches(dm *DeadMessage) bool {
	now := time.Now()
	if len(f.Topics) > 0 && !containsStr(f.Topics, dm.Message.Topic) {
		return false
	}
	if len(f.SenderIDs) > 0 && !containsStr(f.SenderIDs, dm.Message.SenderID) {
		return false
	}
	if f.MinAttempts > 0 && dm.Message.Attempts < f.MinAttempts {
		return false
	}
	if f.MaxAttempts > 0 && dm.Message.Attempts > f.MaxAttempts {
		return false
	}
	if f.OlderThan > 0 && !dm.ArrivedAt.Before(now.Add(-f.OlderThan)) {
		return false
	}
	if f.NewerThan > 0 && !dm.ArrivedAt.After(now.Add(-f.NewerThan)) {
		return false
	}
	if f.errorRe != nil && !f.errorRe.MatchString(dm.LastError()) {
		return false
	}
	for k, v := range f.Tags {
		if dm.Tags[k] != v {
			return false
		}
	}
	if f.ExcludePoison && dm.PoisonPill {
		return false
	}
	return true
}

type TransformFunc func(msg *Message) error

type RedriveProgress struct {
	MessageID string
	Topic     string
	Status    RedriveStatus
	Error     error
	Elapsed   time.Duration
}

type RedriveStatus uint8

const (
	RedriveStatusSuccess RedriveStatus = iota
	RedriveStatusSkipped
	RedriveStatusFailed
	RedriveStatusFiltered
)

type RedriveResult struct {
	Total    int64
	Replayed int64
	Skipped  int64
	Failed   int64
	Duration time.Duration
}

func (r RedriveResult) String() string {
	return fmt.Sprintf("redrive: total=%d replayed=%d skipped=%d failed=%d duration=%s", r.Total, r.Replayed, r.Skipped, r.Failed, r.Duration)
}

type RedriveMetrics interface {
	IncReplayed(topic string)
	IncSkipped(topic string)
	IncFailed(topic string, reason string)
	ObserveDuration(d time.Duration)
	ObserveRate(messagesPerSec float64)
}

type noopRedriveMetrics struct{}

func (noopRedriveMetrics) IncReplayed(string)            {}
func (noopRedriveMetrics) IncSkipped(string)             {}
func (noopRedriveMetrics) IncFailed(string, string)      {}
func (noopRedriveMetrics) ObserveDuration(time.Duration) {}
func (noopRedriveMetrics) ObserveRate(float64)           {}

type RedriveConfig struct {
	Source             Source
	Target             Target
	Transform          TransformFunc
	Workers            int
	RateLimit          float64
	PageSize           int
	DryRun             bool
	ProgressBufferSize int
	SubmitTimeout      time.Duration
	DeleteOnSuccess    bool
	ResetAttempts      bool
	Metrics            RedriveMetrics
	Log                Logger
}

func (c *RedriveConfig) applyDefaults() {
	if c.Workers == 0 {
		c.Workers = 4
	}
	if c.PageSize == 0 {
		c.PageSize = 100
	}
	if c.ProgressBufferSize == 0 {
		c.ProgressBufferSize = 64
	}
	if c.SubmitTimeout == 0 {
		c.SubmitTimeout = 5 * time.Second
	}
	if c.Metrics == nil {
		c.Metrics = noopRedriveMetrics{}
	}
	if c.Log == nil {
		c.Log = discardLogger{}
	}
}

type RedriveOperation struct {
	Progress <-chan RedriveProgress
	Result   <-chan RedriveResult
	pauseCh  chan struct{}
	resumeCh chan struct{}
	cancel   context.CancelFunc
	paused   atomic.Bool
}

func (op *RedriveOperation) Pause() {
	if op.paused.CompareAndSwap(false, true) {
		close(op.pauseCh)
	}
}

func (op *RedriveOperation) Resume() {
	if op.paused.CompareAndSwap(true, false) {
		close(op.resumeCh)
		op.pauseCh = make(chan struct{})
	}
}

func (op *RedriveOperation) Cancel() { op.cancel() }
func (op *RedriveOperation) Wait() RedriveResult {
	return <-op.Result
}

type RedriveEngine struct {
	cfg     RedriveConfig
	running atomic.Bool
}

func NewRedriveEngine(cfg RedriveConfig) (*RedriveEngine, error) {
	if cfg.Source == nil {
		return nil, ErrRedriveNoSource
	}
	cfg.applyDefaults()
	return &RedriveEngine{cfg: cfg}, nil
}

func (re *RedriveEngine) Start(ctx context.Context, filter RedriveFilter) (*RedriveOperation, error) {
	if !re.running.CompareAndSwap(false, true) {
		return nil, ErrRedriveAlreadyRunning
	}
	if err := filter.compile(); err != nil {
		re.running.Store(false)
		return nil, err
	}
	if !re.cfg.DryRun && re.cfg.Target == nil {
		re.running.Store(false)
		return nil, ErrRedriveNoTarget
	}

	opCtx, cancel := context.WithCancel(ctx)
	progressCh := make(chan RedriveProgress, re.cfg.ProgressBufferSize)
	resultCh := make(chan RedriveResult, 1)
	op := &RedriveOperation{
		Progress: progressCh,
		Result:   resultCh,
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
		cancel:   cancel,
	}
	close(op.resumeCh)
	go re.run(opCtx, filter, op, progressCh, resultCh)
	return op, nil
}

func (re *RedriveEngine) RunSync(ctx context.Context, filter RedriveFilter) (RedriveResult, error) {
	op, err := re.Start(ctx, filter)
	if err != nil {
		return RedriveResult{}, err
	}
	go func() {
		for range op.Progress {
		}
	}()
	return op.Wait(), nil
}

func (re *RedriveEngine) run(
	ctx context.Context,
	filter RedriveFilter,
	op *RedriveOperation,
	progressCh chan<- RedriveProgress,
	resultCh chan<- RedriveResult,
) {
	defer re.running.Store(false)
	defer close(progressCh)
	defer close(resultCh)

	cfg := re.cfg
	start := time.Now()
	var total, replayed, skipped, failed atomic.Int64

	var limiter *tokenBucket
	if cfg.RateLimit > 0 {
		limiter = newTokenBucket(cfg.RateLimit, cfg.RateLimit)
	}

	workCh := make(chan *DeadMessage, cfg.Workers*2)
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			re.worker(ctx, cfg, limiter, workCh, progressCh, &total, &replayed, &skipped, &failed)
		}()
	}

	offset := 0
	limit := filter.Limit
	for {
		select {
		case <-op.pauseCh:
			cfg.Log.Info("redrive: paused, waiting for resume")
			select {
			case <-op.resumeCh:
				cfg.Log.Info("redrive: resumed")
			case <-ctx.Done():
				close(workCh)
				wg.Wait()
				resultCh <- RedriveResult{Total: total.Load(), Replayed: replayed.Load(), Skipped: skipped.Load(), Failed: failed.Load(), Duration: time.Since(start)}
				return
			}
		default:
		}
		select {
		case <-ctx.Done():
			close(workCh)
			wg.Wait()
			resultCh <- RedriveResult{Total: total.Load(), Replayed: replayed.Load(), Skipped: skipped.Load(), Failed: failed.Load(), Duration: time.Since(start)}
			return
		default:
		}

		pageSize := cfg.PageSize
		if limit > 0 {
			remaining := limit - int(total.Load())
			if remaining <= 0 {
				break
			}
			if remaining < pageSize {
				pageSize = remaining
			}
		}

		page, err := cfg.Source.List(ctx, DLQFilter{Limit: pageSize, Offset: offset})
		if err != nil {
			cfg.Log.Error("redrive: list failed", "err", err)
			break
		}
		if len(page) == 0 {
			break
		}

		for _, dm := range page {
			if !filter.matches(dm) {
				continue
			}
			select {
			case workCh <- dm:
			case <-ctx.Done():
				close(workCh)
				wg.Wait()
				resultCh <- RedriveResult{Total: total.Load(), Replayed: replayed.Load(), Skipped: skipped.Load(), Failed: failed.Load(), Duration: time.Since(start)}
				return
			}
		}
		if len(page) < pageSize {
			break
		}
		offset += len(page)
	}

	close(workCh)
	wg.Wait()

	dur := time.Since(start)
	cfg.Metrics.ObserveDuration(dur)
	if dur.Seconds() > 0 {
		cfg.Metrics.ObserveRate(float64(replayed.Load()) / dur.Seconds())
	}
	result := RedriveResult{Total: total.Load(), Replayed: replayed.Load(), Skipped: skipped.Load(), Failed: failed.Load(), Duration: dur}
	cfg.Log.Info("redrive: complete", "result", result.String())
	resultCh <- result
}

func (re *RedriveEngine) worker(
	ctx context.Context,
	cfg RedriveConfig,
	limiter *tokenBucket,
	workCh <-chan *DeadMessage,
	progressCh chan<- RedriveProgress,
	total, replayed, skipped, failed *atomic.Int64,
) {
	for dm := range workCh {
		total.Add(1)
		t := time.Now()

		if limiter != nil {
			for !limiter.Allow(1) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}

		msg := cloneMessage(dm.Message)
		if cfg.ResetAttempts {
			msg.Attempts = 0
		}

		if cfg.Transform != nil {
			if err := cfg.Transform(msg); err != nil {
				skipped.Add(1)
				emit(progressCh, RedriveProgress{MessageID: msg.ID, Topic: msg.Topic, Status: RedriveStatusSkipped, Error: err, Elapsed: time.Since(t)})
				continue
			}
		}

		if cfg.DryRun {
			skipped.Add(1)
			emit(progressCh, RedriveProgress{MessageID: msg.ID, Topic: msg.Topic, Status: RedriveStatusSkipped, Elapsed: time.Since(t)})
			continue
		}

		_, cancel := context.WithTimeout(ctx, cfg.SubmitTimeout)
		err := cfg.Target.Submit(msg)
		cancel()
		if err != nil {
			failed.Add(1)
			cfg.Metrics.IncFailed(msg.Topic, err.Error())
			emit(progressCh, RedriveProgress{MessageID: msg.ID, Topic: msg.Topic, Status: RedriveStatusFailed, Error: err, Elapsed: time.Since(t)})
			continue
		}

		replayed.Add(1)
		cfg.Metrics.IncReplayed(msg.Topic)
		if cfg.DeleteOnSuccess {
			delCtx, delCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if delErr := cfg.Source.Delete(delCtx, dm.Message.ID); delErr != nil {
				cfg.Log.Warn("redrive: delete after replay failed", "msgID", msg.ID, "err", delErr)
			}
			delCancel()
		}
		emit(progressCh, RedriveProgress{MessageID: msg.ID, Topic: msg.Topic, Status: RedriveStatusSuccess, Elapsed: time.Since(t)})
	}
}

type RedriveSchedule struct {
	Name       string
	Filter     RedriveFilter
	Interval   time.Duration
	OnComplete func(result RedriveResult)
}

type RedriveScheduler struct {
	engine    *RedriveEngine
	schedules []RedriveSchedule
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
}

func NewRedriveScheduler(engine *RedriveEngine) *RedriveScheduler {
	return &RedriveScheduler{engine: engine, stopCh: make(chan struct{})}
}

func (s *RedriveScheduler) AddSchedule(rs RedriveSchedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules = append(s.schedules, rs)
}

func (s *RedriveScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	schedules := make([]RedriveSchedule, len(s.schedules))
	copy(schedules, s.schedules)
	s.mu.Unlock()
	for _, sched := range schedules {
		s.wg.Add(1)
		go s.runSchedule(ctx, sched)
	}
}

func (s *RedriveScheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *RedriveScheduler) runSchedule(ctx context.Context, rs RedriveSchedule) {
	defer s.wg.Done()
	ticker := time.NewTicker(rs.Interval)
	defer ticker.Stop()
	log := s.engine.cfg.Log

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Info("redrive-scheduler: starting scheduled job", "name", rs.Name)
			result, err := s.engine.RunSync(ctx, rs.Filter)
			if err != nil {
				if errors.Is(err, ErrRedriveAlreadyRunning) {
					log.Warn("redrive-scheduler: skipping - engine busy", "name", rs.Name)
					continue
				}
				log.Error("redrive-scheduler: job error", "name", rs.Name, "err", err)
				continue
			}
			log.Info("redrive-scheduler: job complete", "name", rs.Name, "result", result.String())
			if rs.OnComplete != nil {
				rs.OnComplete(result)
			}
		}
	}
}

func cloneMessage(m *Message) *Message {
	clone := *m
	if m.Body != nil {
		clone.Body = make([]byte, len(m.Body))
		copy(clone.Body, m.Body)
	}
	if m.Metadata != nil {
		clone.Metadata = make(map[string]string, len(m.Metadata))
		for k, v := range m.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

func emit(ch chan<- RedriveProgress, p RedriveProgress) {
	select {
	case ch <- p:
	default:
	}
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
