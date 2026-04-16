package replay

import "context"

type Service struct { runner *Runner }

func NewService(r *Runner) *Service { return &Service{runner: r} }
func (s *Service) Replay(ctx context.Context, selector Selector, rr Range, cp Checkpoint, dryRun bool) ([]Message, Checkpoint, error) {
    return s.runner.Run(ctx, selector, rr, cp, dryRun)
}
