package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flowdesk/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

func fixture(t *testing.T) (*Store, context.Context, domain.User, domain.Workspace) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("нужна отдельная база в TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &Store{Pool: pool}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Register(ctx, "Анна", fmt.Sprintf("test-%d@example.com", time.Now().UnixNano()), "test-hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Exec(c, `DELETE FROM users WHERE id=$1`, u.ID)
	})
	w, err := s.CreateWorkspace(ctx, u.ID, "Тестовая команда")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Exec(c, `DELETE FROM workspaces WHERE id=$1`, w.ID)
	})
	return s, ctx, u, w
}
func newUser(t *testing.T, s *Store, ctx context.Context) domain.User {
	t.Helper()
	u, err := s.Register(ctx, "Участник", fmt.Sprintf("member-%d@example.com", time.Now().UnixNano()), "hash")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.Pool.Exec(c, `DELETE FROM activities WHERE actor_id=$1`, u.ID)
		s.Pool.Exec(c, `DELETE FROM tasks WHERE assignee_id=$1`, u.ID)
		s.Pool.Exec(c, `DELETE FROM users WHERE id=$1`, u.ID)
	})
	return u
}
func input() domain.TaskInput {
	return domain.TaskInput{Title: "Задача", Description: "Контекст", Status: "todo", Priority: "medium"}
}

func TestConcurrentVersionCheck(t *testing.T) {
	s, ctx, u, w := fixture(t)
	task, err := s.SaveTask(ctx, w.ID, u.ID, 0, input())
	if err != nil {
		t.Fatal(err)
	}
	var success, conflict atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := input()
			in.Title = fmt.Sprintf("Изменение %d", i)
			in.Version = task.Version
			_, err := s.SaveTask(ctx, w.ID, u.ID, task.ID, in)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, domain.ErrConflict) {
				conflict.Add(1)
			} else {
				t.Errorf("save: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if success.Load() != 1 || conflict.Load() != 19 {
		t.Fatalf("успехов %d, конфликтов %d", success.Load(), conflict.Load())
	}
	var activities, outbox int
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM activities WHERE workspace_id=$1`, w.ID).Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE workspace_id=$1`, w.ID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if activities != 2 || outbox != 2 {
		t.Fatalf("лишние события: activities=%d outbox=%d", activities, outbox)
	}
	if err = s.DeleteTask(ctx, w.ID, u.ID, task.ID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("устаревшая версия удалила задачу", err)
	}
	if err = s.DeleteTask(ctx, w.ID, u.ID, task.ID, 2); err != nil {
		t.Fatal(err)
	}
}
func TestAccessAndInvitations(t *testing.T) {
	s, ctx, owner, w := fixture(t)
	viewer := newUser(t, s, ctx)
	outsider := newUser(t, s, ctx)
	token, err := s.Invite(ctx, w.ID, owner.ID, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	joined, err := s.Join(ctx, viewer.ID, token)
	if err != nil || joined.Role != "viewer" {
		t.Fatalf("join: %+v %v", joined, err)
	}
	if _, err = s.Join(ctx, outsider.ID, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("одноразовый код использован дважды", err)
	}
	if _, _, err = s.Tasks(ctx, w.ID, outsider.ID, "", 100, 0, "", nil); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("посторонний получил задачи", err)
	}
	if _, err = s.SaveTask(ctx, w.ID, viewer.ID, 0, input()); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("наблюдатель создал задачу", err)
	}
	if _, err = s.Invite(ctx, w.ID, viewer.ID, "editor"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("наблюдатель пригласил участника", err)
	}
	if _, _, err = s.Tasks(ctx, w.ID, viewer.ID, "", 100, 0, "", nil); err != nil {
		t.Fatal("наблюдатель не смог прочитать задачи", err)
	}
	in := input()
	in.AssigneeID = &outsider.ID
	if _, err = s.SaveTask(ctx, w.ID, owner.ID, 0, in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("назначен посторонний пользователь", err)
	}
}

type publisher struct {
	fail bool
	sent int
}

func (p *publisher) Publish(context.Context, int64, string) error {
	if p.fail {
		return errors.New("Redis временно недоступен")
	}
	p.sent++
	return nil
}
func TestOutboxRetryAndSessionRevocation(t *testing.T) {
	s, ctx, u, w := fixture(t)
	if _, err := s.SaveTask(ctx, w.ID, u.ID, 0, input()); err != nil {
		t.Fatal(err)
	}
	p := &publisher{fail: true}
	if _, err := s.Dispatch(ctx, p); err == nil {
		t.Fatal("сбой публикации не замечен")
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE workspace_id=$1`, w.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("событие потеряно при сбое")
	}
	p.fail = false
	if _, err := s.Dispatch(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.sent < 1 {
		t.Fatal("событие не отправлено")
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE workspace_id=$1`, w.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("событие осталось после успешной публикации")
	}
	token, err := s.NewSession(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = s.Pool.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE user_id=$1`, u.ID).Scan(&stored); err != nil || stored == token {
		t.Fatal("в БД должен храниться хеш токена", err)
	}
	if err = s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Session(ctx, token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("отозванная сессия работает", err)
	}
}

func TestFilteringAndPagination(t *testing.T) {
	store, ctx, owner, workspace := fixture(t)
	for i := 0; i < 4; i++ {
		in := input()
		in.Title = fmt.Sprintf("Релиз %d", i)
		in.Priority = "high"
		in.AssigneeID = &owner.ID
		if i == 3 {
			in.Priority = "low"
		}
		if _, err := store.SaveTask(ctx, workspace.ID, owner.ID, 0, in); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := store.Tasks(ctx, workspace.ID, owner.ID, "РЕЛИЗ", 2, 0, "high", &owner.ID)
	if err != nil || total != 3 || len(items) != 2 {
		t.Fatalf("page: %d/%d %v", len(items), total, err)
	}
	last, total, err := store.Tasks(ctx, workspace.ID, owner.ID, "релиз", 2, 2, "high", &owner.ID)
	if err != nil || total != 3 || len(last) != 1 || last[0].ID >= items[1].ID {
		t.Fatalf("next page: %+v %d %v", last, total, err)
	}
	if _, _, err = store.Tasks(ctx, workspace.ID, owner.ID, "", 100, 0, "urgent", nil); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("неверный фильтр принят", err)
	}
}
