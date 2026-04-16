package outbox

import "time"

type Envelope struct {
    Topic          string
    Key            string
    Headers        map[string]string
    Payload        []byte
    ContentType    string
    TraceID        string
    CorrelationID  string
    IdempotencyKey string
    MaxAttempts    int
}

func ToMessage(id string, e Envelope) Message {
    now := time.Now().UTC()
    return Message{ID: id, Topic: e.Topic, Key: e.Key, Headers: e.Headers, Payload: e.Payload, ContentType: e.ContentType, TraceID: e.TraceID, CorrelationID: e.CorrelationID, IdempotencyKey: e.IdempotencyKey, MaxAttempts: e.MaxAttempts, Status: StatusPending, CreatedAt: now, UpdatedAt: now}
}
