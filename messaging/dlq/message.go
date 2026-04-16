package dlq

import "time"

type Message struct {
    ID            string
    Topic         string
    Key           string
    Payload       []byte
    Headers       map[string]string
    Reason        string
    Source        string
    Attempt       int
    FailedAt      time.Time
    TraceID       string
    CorrelationID string
}
