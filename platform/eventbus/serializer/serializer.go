package serializer

import eventbus "github.com/driftappdev/platform/eventbus/codec"

type Serializer struct{ Codec eventbus.Codec }

func (s Serializer) Marshal(v any) ([]byte, error) {
	codec := s.Codec
	if codec == nil {
		codec = eventbus.JSONCodec{}
	}
	return codec.Marshal(v)
}

func (s Serializer) Unmarshal(data []byte, v any) error {
	codec := s.Codec
	if codec == nil {
		codec = eventbus.JSONCodec{}
	}
	return codec.Unmarshal(data, v)
}


