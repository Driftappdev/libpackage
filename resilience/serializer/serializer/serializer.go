package goserializer

import "encoding/json"

func Encode(v any) ([]byte, error)      { return json.Marshal(v) }
func Decode(data []byte, out any) error { return json.Unmarshal(data, out) }
