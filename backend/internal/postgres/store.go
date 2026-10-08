package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"flowdesk/internal/domain"
	"flowdesk/internal/security"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ Pool *pgxpool.Pool }

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(192824)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	paths, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		var applied bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, path).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		raw, err := migrations.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(raw)); err != nil {
			return fmt.Errorf("миграция %s: %w", path, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, path); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func mapError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		if pgErr.Code == "23503" {
			return domain.ErrInvalid
		}
	}
	return err
}

func (s *Store) Register(ctx context.Context, name, email, passwordHash string) (domain.User, error) {
	var u domain.User
	err := s.Pool.QueryRow(ctx, `INSERT INTO users(name,email,password_hash) VALUES($1,$2,$3) RETURNING id,name,email`, name, email, passwordHash).Scan(&u.ID, &u.Name, &u.Email)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		err = domain.ErrEmailTaken
	}
	return u, err
}
func (s *Store) Credentials(ctx context.Context, email string) (domain.User, string, error) {
	var u domain.User
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT id,name,email,password_hash FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Name, &u.Email, &hash)
	return u, hash, mapError(err)
}
func (s *Store) NewSession(ctx context.Context, userID int64) (string, error) {
	token, err := security.Token()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')`, security.Hash(token), userID)
	return token, err
}
func (s *Store) Session(ctx context.Context, token string) (domain.User, error) {
	var u domain.User
	err := s.Pool.QueryRow(ctx, `SELECT u.id,u.name,u.email FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()`, security.Hash(token)).Scan(&u.ID, &u.Name, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrUnauthorized
	}
	return u, err
}
func (s *Store) Logout(ctx context.Context, token string) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, security.Hash(token))
	return err
}

func (s *Store) Workspaces(ctx context.Context, userID int64) ([]domain.Workspace, error) {
	rows, err := s.Pool.Query(ctx, `SELECT w.id,w.name,m.role FROM workspaces w JOIN memberships m ON m.workspace_id=w.id WHERE m.user_id=$1 ORDER BY w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Workspace, 0)
	for rows.Next() {
		var w domain.Workspace
		if err = rows.Scan(&w.ID, &w.Name, &w.Role); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}
func (s *Store) CreateWorkspace(ctx context.Context, userID int64, name string) (domain.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return domain.Workspace{}, domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, err
	}
	defer tx.Rollback(ctx)
	w := domain.Workspace{Name: name, Role: "owner"}
	if err = tx.QueryRow(ctx, `INSERT INTO workspaces(name) VALUES($1) RETURNING id`, name).Scan(&w.ID); err != nil {
		return w, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, w.ID, userID); err != nil {
		return w, err
	}
	return w, tx.Commit(ctx)
}
func (s *Store) Role(ctx context.Context, workspaceID, userID int64) (string, error) {
	var role string
	err := s.Pool.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrForbidden
	}
	return role, err
}
func (s *Store) Members(ctx context.Context, workspaceID, userID int64) ([]domain.Member, error) {
	if _, err := s.Role(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.email,m.role FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 ORDER BY u.name,u.id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Member, 0)
	for rows.Next() {
		var m domain.Member
		if err = rows.Scan(&m.ID, &m.Name, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s *Store) Invite(ctx context.Context, workspaceID, userID int64, role string) (string, error) {
	owner, err := s.Role(ctx, workspaceID, userID)
	if err != nil {
		return "", err
	}
	if owner != "owner" {
		return "", domain.ErrForbidden
	}
	if role != "editor" && role != "viewer" {
		return "", domain.ErrInvalid
	}
	token, err := security.Token()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO invites(token_hash,workspace_id,role,expires_at) VALUES($1,$2,$3,now()+interval '1 day')`, security.Hash(token), workspaceID, role)
	return token, err
}
func (s *Store) Join(ctx context.Context, userID int64, token string) (domain.Workspace, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, err
	}
	defer tx.Rollback(ctx)
	var w domain.Workspace
	err = tx.QueryRow(ctx, `SELECT workspace_id,role FROM invites WHERE token_hash=$1 AND used_at IS NULL AND expires_at>clock_timestamp() FOR UPDATE`, security.Hash(token)).Scan(&w.ID, &w.Role)
	if err != nil {
		return w, mapError(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,$3)`, w.ID, userID, w.Role); err != nil {
		return w, mapError(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE invites SET used_at=now() WHERE token_hash=$1`, security.Hash(token)); err != nil {
		return w, err
	}
	if err = tx.QueryRow(ctx, `SELECT name FROM workspaces WHERE id=$1`, w.ID).Scan(&w.Name); err != nil {
		return w, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO outbox(workspace_id,payload) VALUES($1,'{"type":"members.changed"}')`, w.ID); err != nil {
		return w, err
	}
	return w, tx.Commit(ctx)
}

const taskColumns = `id,workspace_id,title,description,status,priority,assignee_id,due_date,version,updated_at`

func scanTask(row pgx.Row) (domain.Task, error) {
	var t domain.Task
	err := row.Scan(&t.ID, &t.WorkspaceID, &t.Title, &t.Description, &t.Status, &t.Priority, &t.AssigneeID, &t.DueDate, &t.Version, &t.UpdatedAt)
	return t, mapError(err)
}

func (s *Store) Tasks(ctx context.Context, workspaceID, userID int64, search string, limit, offset int, priority string, assignee *int64) ([]domain.Task, int, error) {
	if _, err := s.Role(ctx, workspaceID, userID); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 || offset < 0 || len(search) > 200 {
		return nil, 0, domain.ErrInvalid
	}
	if priority != "" && priority != "low" && priority != "medium" && priority != "high" {
		return nil, 0, domain.ErrInvalid
	}
	if assignee != nil && *assignee < 1 {
		return nil, 0, domain.ErrInvalid
	}
	// Подсчёт и страница читаются из одного снимка базы.
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	var total int
	const filter = `workspace_id=$1 AND strpos(lower(title),lower($2))>0 AND ($3::text='' OR priority=$3) AND ($4::bigint IS NULL OR assignee_id=$4)`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE `+filter, workspaceID, search, priority, assignee).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, `SELECT `+taskColumns+` FROM tasks WHERE `+filter+` ORDER BY id DESC LIMIT $5 OFFSET $6`, workspaceID, search, priority, assignee, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	result := make([]domain.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		result = append(result, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return result, total, tx.Commit(ctx)
}

func (s *Store) SaveTask(ctx context.Context, workspaceID, userID, taskID int64, input domain.TaskInput) (domain.Task, error) {
	if err := input.Validate(); err != nil {
		return domain.Task{}, err
	}
	if taskID != 0 && input.Version < 1 {
		return domain.Task{}, domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, domain.ErrForbidden
	}
	if err != nil {
		return domain.Task{}, err
	}
	if !domain.CanEdit(role) {
		return domain.Task{}, domain.ErrForbidden
	}
	var task domain.Task
	action := "created"
	if taskID == 0 {
		task, err = scanTask(tx.QueryRow(ctx, `INSERT INTO tasks(workspace_id,title,description,status,priority,assignee_id,due_date) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+taskColumns, workspaceID, input.Title, input.Description, input.Status, input.Priority, input.AssigneeID, input.DueDate))
	} else {
		action = "updated"
		task, err = scanTask(tx.QueryRow(ctx, `UPDATE tasks SET title=$3,description=$4,status=$5,priority=$6,assignee_id=$7,due_date=$8,version=version+1,updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2 AND version=$9 RETURNING `+taskColumns, workspaceID, taskID, input.Title, input.Description, input.Status, input.Priority, input.AssigneeID, input.DueDate, input.Version))
		if errors.Is(err, domain.ErrNotFound) {
			var exists bool
			if queryErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE workspace_id=$1 AND id=$2)`, workspaceID, taskID).Scan(&exists); queryErr != nil {
				return task, queryErr
			}
			if exists {
				err = domain.ErrConflict
			}
		}
	}
	if err != nil {
		return task, err
	}
	if err = record(ctx, tx, task, userID, action); err != nil {
		return task, err
	}
	return task, tx.Commit(ctx)
}

func (s *Store) DeleteTask(ctx context.Context, workspaceID, userID, taskID int64, version int) error {
	if version < 1 {
		return domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrForbidden
	}
	if err != nil {
		return err
	}
	if !domain.CanEdit(role) {
		return domain.ErrForbidden
	}
	task, err := scanTask(tx.QueryRow(ctx, `DELETE FROM tasks WHERE workspace_id=$1 AND id=$2 AND version=$3 RETURNING `+taskColumns, workspaceID, taskID, version))
	if errors.Is(err, domain.ErrNotFound) {
		var exists bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE workspace_id=$1 AND id=$2)`, workspaceID, taskID).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return domain.ErrConflict
		}
	}
	if err != nil {
		return err
	}
	if err = record(ctx, tx, task, userID, "deleted"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func record(ctx context.Context, tx pgx.Tx, t domain.Task, userID int64, action string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO activities(workspace_id,task_id,actor_id,action,title) VALUES($1,$2,$3,$4,$5)`, t.WorkspaceID, t.ID, userID, action, t.Title); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO outbox(workspace_id,payload) VALUES($1,jsonb_build_object('type','tasks.changed','task_id',$2::bigint,'version',$3::integer))`, t.WorkspaceID, t.ID, t.Version)
	return err
}
func (s *Store) Activities(ctx context.Context, workspaceID, userID int64) ([]domain.Activity, error) {
	if _, err := s.Role(ctx, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.id,a.task_id,u.name,a.action,a.title,a.created_at FROM activities a JOIN users u ON u.id=a.actor_id WHERE a.workspace_id=$1 ORDER BY a.id DESC LIMIT 50`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Activity, 0)
	for rows.Next() {
		var a domain.Activity
		if err = rows.Scan(&a.ID, &a.TaskID, &a.Actor, &a.Action, &a.Title, &a.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

type Publisher interface {
	Publish(context.Context, int64, string) error
}

func (s *Store) Dispatch(ctx context.Context, p Publisher) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,workspace_id,payload::text FROM outbox ORDER BY id LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return 0, err
	}
	type event struct {
		id, workspaceID int64
		payload         string
	}
	events := make([]event, 0)
	for rows.Next() {
		var e event
		if err = rows.Scan(&e.id, &e.workspaceID, &e.payload); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, e := range events {
		// Публикация может повториться после сбоя перед Commit. Клиент перечитывает данные, а не применяет событие дважды.
		if err = p.Publish(ctx, e.workspaceID, e.payload); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `DELETE FROM outbox WHERE id=$1`, e.id); err != nil {
			return 0, err
		}
	}
	return len(events), tx.Commit(ctx)
}
func (s *Store) CleanupSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at<=now()`)
	return err
}
