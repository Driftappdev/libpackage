package goevaluator

import "hash/fnv"

func PickVariant(subjectKey string, variants int) int {
	if variants <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(subjectKey))
	return int(h.Sum32() % uint32(variants))
}
