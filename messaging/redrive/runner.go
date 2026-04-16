package redrive

import (
    "context"

    "github.com/driftappdev/libpackage/messaging/dlq"
)

type Publisher interface { Publish(context.Context, dlq.Message) error }

type Runner struct {
    store     dlq.Store
    publisher Publisher
    policy    Policy
}

func NewRunner(store dlq.Store, publisher Publisher, policy Policy) *Runner { return &Runner{store: store, publisher: publisher, policy: policy} }
func (r *Runner) Run(ctx context.Context, messages []dlq.Message, dryRun bool) error {
    if dryRun { return nil }
    if r.publisher == nil { return ErrPublisherMissing }
    for _, msg := range messages {
        if err := r.publisher.Publish(ctx, msg); err != nil { return err }
        if r.policy.DeleteOnSuccess { _ = r.store.Delete(ctx, msg.ID) }
    }
    return nil
}
