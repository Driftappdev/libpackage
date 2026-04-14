package tracing

import "hash/fnv"

type Sampler interface{ ShouldSample(traceID string) bool }

type RatioSampler struct{ Rate uint32 }

func (s RatioSampler) ShouldSample(traceID string) bool {
	if s.Rate == 0 {
		return false
	}
	if s.Rate >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(traceID))
	return h.Sum32()%100 < s.Rate
}
