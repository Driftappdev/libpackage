package gohooks

import "context"

type Hook func(context.Context) error

type Set struct {
	before []Hook
	after  []Hook
}

func (s *Set) Before(h Hook) { s.before = append(s.before, h) }
func (s *Set) After(h Hook)  { s.after = append(s.after, h) }
