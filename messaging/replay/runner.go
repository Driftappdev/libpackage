package replay

import "context"

type Message struct {
	ID      string
	Topic   string
	Key     string
	Payload []byte
}

type Source interface {
	Fetch(context.Context, Selector, Range, Checkpoint) ([]Message, Checkpoint, error)
}
type Publisher interface {
	Publish(context.Context, Message) error
}

type Runner struct {
	source    Source
	publisher Publisher
}

func NewRunner(source Source, publisher Publisher) *Runner {
	return &Runner{source: source, publisher: publisher}
}
func (r *Runner) Run(ctx context.Context, selector Selector, rr Range, cp Checkpoint, dryRun bool) ([]Message, Checkpoint, error) {
	if r.source == nil {
		return nil, Checkpoint{}, ErrSourceMissing
	}
	msgs, next, err := r.source.Fetch(ctx, selector, rr, cp)
	if err != nil {
		return nil, Checkpoint{}, err
	}
	if dryRun || r.publisher == nil {
		return msgs, next, nil
	}
	for _, msg := range msgs {
		if err := r.publisher.Publish(ctx, msg); err != nil {
			return msgs, next, err
		}
	}
	return msgs, next, nil
}
