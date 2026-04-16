package inbox

type Status string

const (
    StatusReceived   Status = "received"
    StatusProcessing Status = "processing"
    StatusProcessed  Status = "processed"
    StatusFailed     Status = "failed"
)
