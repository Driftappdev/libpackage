package exampleintegration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/driftappdev/messaging/dlq"
	"github.com/driftappdev/messaging/inbox"
	"github.com/driftappdev/messaging/outbox"
	"github.com/driftappdev/messaging/redrive"
)

type SlogAdapter struct{ l *slog.Logger }

func NewSlogAdapter(l *slog.Logger) *SlogAdapter     { return &SlogAdapter{l: l} }
func (a *SlogAdapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a *SlogAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a *SlogAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }

func HTTPRelayFunc(endpoint string) outbox.RelayFunc {
	return func(ctx context.Context, batch []*outbox.Message) error {
		_ = endpoint
		_ = ctx
		_ = batch
		return nil
	}
}

func KafkaRelayFunc(topic string) outbox.RelayFunc {
	return func(ctx context.Context, batch []*outbox.Message) error {
		_ = topic
		_ = ctx
		_ = batch
		return nil
	}
}

type PrometheusInboxMetrics struct{}

func (m *PrometheusInboxMetrics) IncReceived(topic string, p inbox.Priority) { _ = topic; _ = p }
func (m *PrometheusInboxMetrics) IncDropped(topic string, reason string)     { _ = topic; _ = reason }
func (m *PrometheusInboxMetrics) IncDuplicate(topic string)                  { _ = topic }
func (m *PrometheusInboxMetrics) ObserveQueueDepth(p inbox.Priority, depth int) {
	_ = p
	_ = depth
}
func (m *PrometheusInboxMetrics) ObserveLatency(stage string, d time.Duration) { _ = stage; _ = d }

type Log interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type ServicePipeline struct {
	Inbox   *inbox.Inbox
	Outbox  *outbox.Outbox
	DLQ     *dlq.DLQ
	Redrive *redrive.RedriveEngine
	log     Log
}

type inboxAsDLQTarget struct {
	in *inbox.Inbox
}

func (a *inboxAsDLQTarget) Submit(msg *dlq.Message) error {
	return a.in.Submit(toInboxMessageFromDLQ(msg))
}

type dlqSinkAdapter struct {
	d *dlq.DLQ
}

func (a *dlqSinkAdapter) Send(ctx context.Context, msg *outbox.Message, lastErr error) error {
	return a.d.Send(ctx, toDLQMessageFromOutbox(msg), lastErr)
}

type dlqSourceAdapter struct {
	d *dlq.DLQ
}

func (a *dlqSourceAdapter) List(ctx context.Context, f redrive.DLQFilter) ([]*redrive.DeadMessage, error) {
	items, err := a.d.List(ctx, dlq.DLQFilter{
		Topic:       f.Topic,
		PoisonOnly:  f.PoisonOnly,
		Before:      f.Before,
		After:       f.After,
		ExpiredOnly: f.ExpiredOnly,
		Limit:       f.Limit,
		Offset:      f.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*redrive.DeadMessage, 0, len(items))
	for _, it := range items {
		out = append(out, toRedriveDeadMessage(it))
	}
	return out, nil
}

func (a *dlqSourceAdapter) Delete(ctx context.Context, id string) error {
	return a.d.Delete(ctx, id)
}

type inboxTargetAdapter struct {
	in *inbox.Inbox
}

func (a *inboxTargetAdapter) Submit(msg *redrive.Message) error {
	return a.in.Submit(toInboxMessageFromRedrive(msg))
}

func NewServicePipeline(
	relay outbox.RelayFunc,
	store outbox.PersistenceStore,
	dlqStorage dlq.DLQStorage,
	log Log,
) (*ServicePipeline, error) {
	if log == nil {
		log = NewSlogAdapter(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	}
	if dlqStorage == nil {
		dlqStorage = dlq.NewInMemoryDLQStorage()
	}

	inboxCore := inbox.NewInbox(inbox.InboxConfig{
		BufferSizes:        [4]int{512, 2048, 8192, 16384},
		OverflowStrategy:   inbox.OverflowReject,
		RateLimitCapacity:  5000,
		RateLimitRate:      2000,
		CBFailureThreshold: 20,
		CBSuccessThreshold: 5,
		CBOpenTimeout:      60 * time.Second,
		DedupeEnabled:      true,
		DefaultMaxAttempts: 5,
		DrainTimeout:       30 * time.Second,
		Log:                log,
	})

	dlqCore, err := dlq.NewDLQ(dlq.DLQConfig{
		Storage:         dlqStorage,
		Log:             log,
		DefaultTTL:      72 * time.Hour,
		MaxMessages:     100_000,
		PoisonThreshold: 5,
		ReaperInterval:  10 * time.Minute,
		ReplayInbox:     &inboxAsDLQTarget{in: inboxCore},
	})
	if err != nil {
		return nil, fmt.Errorf("pipeline: dlq: %w", err)
	}

	outboxCore, err := outbox.NewOutbox(outbox.OutboxConfig{
		Relay:               relay,
		Store:               store,
		DLQ:                 &dlqSinkAdapter{d: dlqCore},
		Backoff:             outbox.NewExponentialJitterBackoff(200*time.Millisecond, 60*time.Second),
		WorkerCount:         8,
		MaxBatchSize:        200,
		BatchLingerDuration: 10 * time.Millisecond,
		MaxRelayAttempts:    7,
		RelayTimeout:        15 * time.Second,
		DrainTimeout:        45 * time.Second,
		ReloadPending:       true,
		Log:                 log,
	})
	if err != nil {
		dlqCore.Close()
		return nil, fmt.Errorf("pipeline: outbox: %w", err)
	}

	redriveCore, err := redrive.NewRedriveEngine(redrive.RedriveConfig{
		Source:          &dlqSourceAdapter{d: dlqCore},
		Target:          &inboxTargetAdapter{in: inboxCore},
		Workers:         4,
		RateLimit:       500,
		PageSize:        200,
		DeleteOnSuccess: true,
		ResetAttempts:   true,
		Log:             log,
	})
	if err != nil {
		_ = outboxCore.Close()
		dlqCore.Close()
		return nil, fmt.Errorf("pipeline: redrive: %w", err)
	}

	return &ServicePipeline{
		Inbox:   inboxCore,
		Outbox:  outboxCore,
		DLQ:     dlqCore,
		Redrive: redriveCore,
		log:     log,
	}, nil
}

func toDLQMessageFromOutbox(m *outbox.Message) *dlq.Message {
	return &dlq.Message{
		ID:          m.ID,
		SenderID:    m.SenderID,
		Topic:       m.Topic,
		Priority:    dlq.Priority(m.Priority),
		Body:        cloneBytes(m.Body),
		Metadata:    cloneMap(m.Metadata),
		Timestamp:   m.Timestamp,
		Attempts:    m.Attempts,
		MaxAttempts: m.MaxAttempts,
	}
}

func toDLQMessageFromInbox(m *inbox.Message) *dlq.Message {
	return &dlq.Message{
		ID:          m.ID,
		SenderID:    m.SenderID,
		Topic:       m.Topic,
		Priority:    dlq.Priority(m.Priority),
		Body:        cloneBytes(m.Body),
		Metadata:    cloneMap(m.Metadata),
		Timestamp:   m.Timestamp,
		Attempts:    m.Attempts,
		MaxAttempts: m.MaxAttempts,
	}
}

func toInboxMessageFromDLQ(m *dlq.Message) *inbox.Message {
	return &inbox.Message{
		ID:          m.ID,
		SenderID:    m.SenderID,
		Topic:       m.Topic,
		Priority:    inbox.Priority(m.Priority),
		Body:        cloneBytes(m.Body),
		Metadata:    cloneMap(m.Metadata),
		Timestamp:   m.Timestamp,
		Attempts:    m.Attempts,
		MaxAttempts: m.MaxAttempts,
	}
}

func toInboxMessageFromRedrive(m *redrive.Message) *inbox.Message {
	return &inbox.Message{
		ID:          m.ID,
		SenderID:    m.SenderID,
		Topic:       m.Topic,
		Priority:    inbox.Priority(m.Priority),
		Body:        cloneBytes(m.Body),
		Metadata:    cloneMap(m.Metadata),
		Timestamp:   m.Timestamp,
		Attempts:    m.Attempts,
		MaxAttempts: m.MaxAttempts,
	}
}

func toRedriveDeadMessage(dm *dlq.DeadMessage) *redrive.DeadMessage {
	failures := make([]redrive.FailureRecord, 0, len(dm.Failures))
	for _, f := range dm.Failures {
		failures = append(failures, redrive.FailureRecord{
			AttemptedAt: f.AttemptedAt,
			Error:       f.Error,
			Attempt:     f.Attempt,
		})
	}
	return &redrive.DeadMessage{
		Message: &redrive.Message{
			ID:          dm.Message.ID,
			SenderID:    dm.Message.SenderID,
			Topic:       dm.Message.Topic,
			Priority:    redrive.Priority(dm.Message.Priority),
			Body:        cloneBytes(dm.Message.Body),
			Metadata:    cloneMap(dm.Message.Metadata),
			Timestamp:   dm.Message.Timestamp,
			Attempts:    dm.Message.Attempts,
			MaxAttempts: dm.Message.MaxAttempts,
		},
		Failures:   failures,
		ArrivedAt:  dm.ArrivedAt,
		ExpiresAt:  dm.ExpiresAt,
		PoisonPill: dm.PoisonPill,
		Replayed:   dm.Replayed,
		Tags:       cloneMap(dm.Tags),
	}
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (p *ServicePipeline) Close() {
	if err := p.Inbox.Close(); err != nil && !errors.Is(err, inbox.ErrInboxClosed) {
		p.log.Error("pipeline: inbox close", "err", err)
	}
	if err := p.Outbox.Close(); err != nil && !errors.Is(err, outbox.ErrOutboxClosed) {
		p.log.Error("pipeline: outbox close", "err", err)
	}
	p.DLQ.Close()
}

type ConsumerHandlerFunc func(ctx context.Context, msg *inbox.Message) error

func (p *ServicePipeline) StartConsumers(ctx context.Context, workers int, handler ConsumerHandlerFunc) {
	for i := 0; i < workers; i++ {
		go func(id int) {
			for {
				msg, err := p.Inbox.Consume(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					if errors.Is(err, inbox.ErrInboxClosed) {
						return
					}
					p.log.Error("consumer: inbox error", "worker", id, "err", err)
					return
				}

				msg.Attempts++
				if hErr := handler(ctx, msg); hErr != nil {
					if msg.Attempts >= msg.MaxAttempts {
						dlqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
						_ = p.DLQ.Send(dlqCtx, toDLQMessageFromInbox(msg), hErr)
						cancel()
					} else {
						requeue := *msg
						if requeue.Priority < inbox.PriorityLow {
							requeue.Priority++
						}
						_ = p.Inbox.Submit(&requeue)
					}
				}
			}
		}(i)
	}
}

func (p *ServicePipeline) StartAutoRedrive(ctx context.Context, interval time.Duration) *redrive.RedriveScheduler {
	scheduler := redrive.NewRedriveScheduler(p.Redrive)
	scheduler.AddSchedule(redrive.RedriveSchedule{
		Name:     "auto-redrive",
		Interval: interval,
		Filter: redrive.RedriveFilter{
			OlderThan:     5 * time.Minute,
			MaxAttempts:   10,
			ExcludePoison: false,
		},
		OnComplete: func(r redrive.RedriveResult) {
			p.log.Info("auto-redrive complete", "replayed", r.Replayed, "failed", r.Failed, "duration", r.Duration)
		},
	})
	scheduler.Start(ctx)
	return scheduler
}

func (p *ServicePipeline) RunUntilSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-sigCh
	p.log.Info("pipeline: received signal, shutting down", "signal", sig.String())
	p.Close()
}

