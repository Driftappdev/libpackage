package outbox

import "time"

type Message struct {
    ID             string
    Topic          string
    Key            string
    Headers        map[string]string
    Payload        []byte
    ContentType    string
    TraceID        string
    CorrelationID  string
    IdempotencyKey string
    Attempt        int
    MaxAttempts    int
    NextAttemptAt  time.Time
    Status         Status
    Error          string
    CreatedAt      time.Time
    UpdatedAt      time.Time
    PublishedAt    *time.Time
}
