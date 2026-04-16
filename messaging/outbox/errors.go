package outbox

import "errors"

var (
    ErrMessageNotFound   = errors.New("outbox: message not found")
    ErrInvalidState      = errors.New("outbox: invalid state transition")
    ErrEncoding          = errors.New("outbox: encoding failed")
    ErrDispatchExhausted = errors.New("outbox: dispatch attempts exhausted")
)
