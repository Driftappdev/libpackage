package replay

import "time"

type Checkpoint struct { Cursor string; LastEventAt time.Time }
