package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"flowdesk/internal/postgres"
	"github.com/redis/go-redis/v9"
)

type Publisher struct{ Redis *redis.Client }

func Channel(workspaceID int64) string { return fmt.Sprintf("workspace:%d", workspaceID) }
func (p Publisher) Publish(ctx context.Context, workspaceID int64, payload string) error {
	return p.Redis.Publish(ctx, Channel(workspaceID), payload).Err()
}

type Dispatcher interface {
	Dispatch(context.Context, postgres.Publisher) (int, error)
	CleanupSessions(context.Context) error
}

func Run(ctx context.Context, store Dispatcher, p postgres.Publisher) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	cleanup := time.NewTicker(time.Hour)
	defer cleanup.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			job, cancel := context.WithTimeout(ctx, 5*time.Second)
			count, err := store.Dispatch(job, p)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.Warn("события пока не отправлены, повторим", "error", err)
			}
			if count > 0 {
				slog.Info("события отправлены", "count", count)
			}
		case <-cleanup.C:
			job, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := store.CleanupSessions(job)
			cancel()
			if err != nil {
				slog.Warn("не удалось очистить сессии", "error", err)
			}
		}
	}
}
