package events

import (
	"context"
	"flowdesk/internal/postgres"
	"testing"
	"time"
)

type dispatcher struct{ called chan struct{} }

func (d dispatcher) Dispatch(context.Context, postgres.Publisher) (int, error) {
	select {
	case d.called <- struct{}{}:
	default:
	}
	return 0, nil
}
func (d dispatcher) CleanupSessions(context.Context) error { return nil }

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, int64, string) error { return nil }
func TestWorkerShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() { Run(ctx, dispatcher{called}, noopPublisher{}); close(done) }()
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("обработчик не запустился")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("обработчик не остановился")
	}
}
