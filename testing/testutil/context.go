package testutil

import (
	"context"
	"testing"
	"time"
)

func Context(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}
