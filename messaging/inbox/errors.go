package inbox

import "errors"

var (
    ErrDuplicateMessage = errors.New("inbox: duplicate message")
    ErrHandlerNotFound  = errors.New("inbox: handler not found")
    ErrMessageNotFound  = errors.New("inbox: message not found")
)
