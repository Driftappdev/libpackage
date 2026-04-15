package shutdown

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func NotifyContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
}

func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, timeout)
}

func Server(parent context.Context, server *http.Server, timeout time.Duration) error {
	ctx, cancel := WithTimeout(parent, timeout)
	defer cancel()
	return server.Shutdown(ctx)
}

func Signals(sig ...os.Signal) <-chan os.Signal {
	if len(sig) == 0 {
		sig = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig...)
	return ch
}
