package dlq

import "errors"

var ErrNotFound = errors.New("dlq: message not found")
