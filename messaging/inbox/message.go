package inbox

import "time"

type Message struct {
    ID            string
    Topic         string
    Key           string
    Headers       map[string]string
    Payload       []byte
    ContentType   string
    TraceID       string
    CorrelationID string
    ReceivedAt    time.Time
    ProcessedAt   *time.Time
    Status        Status
    Error         string
}
