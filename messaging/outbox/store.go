package outbox

import (
    "context"
    "sync"
    "time"
)

type Store interface {
    Save(context.Context, Message) error
    Get(context.Context, string) (Message, error)
    ReserveBatch(context.Context, int, time.Duration) ([]Message, error)
    MarkPublished(context.Context, string, time.Time) error
    MarkFailed(context.Context, string, error, time.Time, int) error
    MarkDeadLetter(context.Context, string, error) error
}

type MemoryStore struct {
    mu   sync.Mutex
    data map[string]Message
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{data: make(map[string]Message)} }
func (s *MemoryStore) Save(_ context.Context, msg Message) error { s.mu.Lock(); defer s.mu.Unlock(); s.data[msg.ID] = msg; return nil }
func (s *MemoryStore) Get(_ context.Context, id string) (Message, error) { s.mu.Lock(); defer s.mu.Unlock(); msg, ok := s.data[id]; if !ok { return Message{}, ErrMessageNotFound }; return msg, nil }
func (s *MemoryStore) ReserveBatch(_ context.Context, batch int, reserveFor time.Duration) ([]Message, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    out := make([]Message, 0, batch)
    now := time.Now().UTC()
    for id, msg := range s.data {
        if len(out) >= batch { break }
        if msg.Status == StatusPending || (msg.Status == StatusFailed && !msg.NextAttemptAt.After(now)) {
            msg.Status = StatusReserved
            msg.UpdatedAt = now.Add(reserveFor)
            s.data[id] = msg
            out = append(out, msg)
        }
    }
    return out, nil
}
func (s *MemoryStore) MarkPublished(_ context.Context, id string, at time.Time) error { s.mu.Lock(); defer s.mu.Unlock(); msg, ok := s.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusPublished; msg.PublishedAt = &at; msg.UpdatedAt = at; s.data[id] = msg; return nil }
func (s *MemoryStore) MarkFailed(_ context.Context, id string, cause error, next time.Time, attempts int) error { s.mu.Lock(); defer s.mu.Unlock(); msg, ok := s.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusFailed; msg.Error = cause.Error(); msg.Attempt = attempts; msg.NextAttemptAt = next; msg.UpdatedAt = time.Now().UTC(); s.data[id] = msg; return nil }
func (s *MemoryStore) MarkDeadLetter(_ context.Context, id string, cause error) error { s.mu.Lock(); defer s.mu.Unlock(); msg, ok := s.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusDeadLetter; msg.Error = cause.Error(); msg.UpdatedAt = time.Now().UTC(); s.data[id] = msg; return nil }
