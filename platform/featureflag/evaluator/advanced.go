package evaluator

import (
	"context"

	ffclient "github.com/driftappdev/platform/featureflag/client"
)

type Subject struct {
	ID         string
	Attributes map[string]string
}

type Evaluator struct {
	Client *ffclient.StoreClient
}

func (e Evaluator) Bool(ctx context.Context, key string, subject Subject, fallback bool) bool {
	flag, ok := e.Client.Get(ctx, key)
	if !ok {
		return fallback
	}
	if len(flag.Rules) == 0 {
		return flag.Enabled
	}
	for k, v := range flag.Rules {
		if subject.Attributes[k] != v {
			return fallback
		}
	}
	return flag.Enabled
}

func (e Evaluator) Variant(ctx context.Context, key string, subject Subject, fallback string) string {
	flag, ok := e.Client.Get(ctx, key)
	if !ok {
		return fallback
	}
	if len(flag.Rules) == 0 {
		if flag.Variant != "" {
			return flag.Variant
		}
		return fallback
	}
	for k, v := range flag.Rules {
		if subject.Attributes[k] != v {
			return fallback
		}
	}
	if flag.Variant != "" {
		return flag.Variant
	}
	return fallback
}


