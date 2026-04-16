package inbox

import (
    "context"
    "sync"
    "time"
)

type Repository interface {
    Save(context.Context, Message) error
    Get(context.Context, string) (Message, error)
    MarkProcessing(context.Context, string) error
    MarkProcessed(context.Context, string, time.Time) error
    MarkFailed(context.Context, string, error) error
}

type MemoryRepository struct {
    mu   sync.Mutex
    data map[string]Message
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{data: make(map[string]Message)} }
func (r *MemoryRepository) Save(_ context.Context, msg Message) error { r.mu.Lock(); defer r.mu.Unlock(); r.data[msg.ID] = msg; return nil }
func (r *MemoryRepository) Get(_ context.Context, id string) (Message, error) { r.mu.Lock(); defer r.mu.Unlock(); msg, ok := r.data[id]; if !ok { return Message{}, ErrMessageNotFound }; return msg, nil }
func (r *MemoryRepository) MarkProcessing(_ context.Context, id string) error { r.mu.Lock(); defer r.mu.Unlock(); msg, ok := r.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusProcessing; r.data[id] = msg; return nil }
func (r *MemoryRepository) MarkProcessed(_ context.Context, id string, at time.Time) error { r.mu.Lock(); defer r.mu.Unlock(); msg, ok := r.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusProcessed; msg.ProcessedAt = &at; r.data[id] = msg; return nil }
func (r *MemoryRepository) MarkFailed(_ context.Context, id string, cause error) error { r.mu.Lock(); defer r.mu.Unlock(); msg, ok := r.data[id]; if !ok { return ErrMessageNotFound }; msg.Status = StatusFailed; msg.Error = cause.Error(); r.data[id] = msg; return nil }
