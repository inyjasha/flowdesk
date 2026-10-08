package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"flowdesk/internal/auth"
	"flowdesk/internal/events"
	"flowdesk/internal/httpapi"
	"flowdesk/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("приложение остановлено", "error", err)
		os.Exit(1)
	}
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("нужна DATABASE_URL")
	}
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(startup, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(startup); err != nil {
		return err
	}
	store := &postgres.Store{Pool: pool}
	if err = store.Migrate(startup); err != nil {
		return err
	}
	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "localhost:6379"), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 1})
	defer rdb.Close()
	if err = rdb.Ping(startup).Err(); err != nil {
		return err
	}
	authService, err := auth.New(store)
	if err != nil {
		return err
	}
	a := &httpapi.API{Store: store, Auth: authService, Redis: rdb, Origin: env("PUBLIC_URL", "http://localhost:8090"), SecureCookie: env("COOKIE_SECURE", "false") == "true", StaticDir: env("STATIC_DIR", ""), Shutdown: ctx, Ready: func(c context.Context) error {
		if err := pool.Ping(c); err != nil {
			return err
		}
		return rdb.Ping(c).Err()
	}}
	server := &http.Server{Addr: env("HTTP_ADDR", ":8090"), Handler: a.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	// Для SSE WriteTimeout задаётся перед каждой записью, а не на всё время соединения.
	var wg sync.WaitGroup
	wg.Add(1)
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() { defer wg.Done(); events.Run(workerCtx, store, events.Publisher{Redis: rdb}) }()
	done := make(chan error, 1)
	go func() { slog.Info("FlowDesk запущен", "address", server.Addr); done <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}
	stop()
	stopWorker()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	shutdownErr := server.Shutdown(shutdown)
	if shutdownErr != nil {
		server.Close()
	}
	wg.Wait()
	return errors.Join(err, shutdownErr)
}
