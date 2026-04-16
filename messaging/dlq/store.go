package dlq

import (
    "context"
    "sync"
)

type Store interface {
    Put(context.Context, Message) error
    Get(context.Context, string) (Message, error)
    List(context.Context, Filter) ([]Message, error)
    Delete(context.Context, string) error
}

type Filter struct { Topic string; Source string }

type MemoryStore struct {
    mu   sync.Mutex
    data map[string]Message
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: make(map[string]Message)} }
func (s *MemoryStore) Put(_ context.Context, msg Message) error { s.mu.Lock(); defer s.mu.Unlock(); s.data[msg.ID] = msg; return nil }
func (s *MemoryStore) Get(_ context.Context, id string) (Message, error) { s.mu.Lock(); defer s.mu.Unlock(); msg, ok := s.data[id]; if !ok { return Message{}, ErrNotFound }; return msg, nil }
func (s *MemoryStore) List(_ context.Context, f Filter) ([]Message, error) { s.mu.Lock(); defer s.mu.Unlock(); out := make([]Message, 0, len(s.data)); for _, msg := range s.data { if f.Topic != "" && msg.Topic != f.Topic { continue }; if f.Source != "" && msg.Source != f.Source { continue }; out = append(out, msg) }; return out, nil }
func (s *MemoryStore) Delete(_ context.Context, id string) error { s.mu.Lock(); defer s.mu.Unlock(); delete(s.data, id); return nil }
