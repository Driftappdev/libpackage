package redrive

import (
    "context"

    "github.com/driftappdev/libpackage/messaging/dlq"
)

type Service struct { store dlq.Store; runner *Runner }

func NewService(store dlq.Store, runner *Runner) *Service { return &Service{store: store, runner: runner} }
func (s *Service) Redrive(ctx context.Context, req Request, f Filter) ([]dlq.Message, error) {
    selected := make([]dlq.Message, 0)
    if len(req.IDs) > 0 {
        for _, id := range req.IDs {
            msg, err := s.store.Get(ctx, id)
            if err != nil { return nil, err }
            selected = append(selected, msg)
        }
    } else {
        list, err := s.store.List(ctx, dlq.Filter{Topic: f.Topic, Source: f.Source})
        if err != nil { return nil, err }
        selected = list
    }
    if s.runner != nil { if err := s.runner.Run(ctx, selected, req.DryRun); err != nil { return nil, err } }
    return selected, nil
}
