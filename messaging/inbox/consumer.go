package inbox

import "context"

type Consumer interface { Receive(context.Context) (Message, error) }
