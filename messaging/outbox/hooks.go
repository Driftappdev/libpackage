package outbox

import "context"

type Hooks struct {
    BeforeDispatch func(context.Context, Message) error
    AfterDispatch  func(context.Context, Message) error
    OnFailure      func(context.Context, Message, error) error
}
