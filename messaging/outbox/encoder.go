package outbox

import "encoding/json"

type Encoder interface { Encode(any) ([]byte, string, error) }

type JSONEncoder struct{}

func (JSONEncoder) Encode(v any) ([]byte, string, error) {
    b, err := json.Marshal(v)
    if err != nil { return nil, "", ErrEncoding }
    return b, "application/json", nil
}
