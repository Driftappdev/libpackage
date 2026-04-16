package outbox

type Status string

const (
    StatusPending    Status = "pending"
    StatusReserved   Status = "reserved"
    StatusPublished  Status = "published"
    StatusFailed     Status = "failed"
    StatusDeadLetter Status = "dead_letter"
)
