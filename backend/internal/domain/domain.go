package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid      = errors.New("некорректные данные")
	ErrUnauthorized = errors.New("нужен вход в аккаунт")
	ErrForbidden    = errors.New("недостаточно прав")
	ErrNotFound     = errors.New("объект не найден")
	ErrConflict     = errors.New("данные уже изменились, обновите страницу")
	ErrEmailTaken   = errors.New("этот email уже зарегистрирован")
)

type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
type Workspace struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type Member struct {
	User
	Role string `json:"role"`
}
type Task struct {
	ID          int64      `json:"id"`
	WorkspaceID int64      `json:"workspace_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	AssigneeID  *int64     `json:"assignee_id"`
	DueDate     *time.Time `json:"due_date"`
	Version     int        `json:"version"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
type Activity struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}
type TaskInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	AssigneeID  *int64     `json:"assignee_id"`
	DueDate     *time.Time `json:"due_date"`
	Version     int        `json:"version"`
}

func (i *TaskInput) Validate() error {
	i.Title = strings.TrimSpace(i.Title)
	if i.Title == "" || len(i.Title) > 200 || len(i.Description) > 5000 {
		return ErrInvalid
	}
	if i.Status != "todo" && i.Status != "in_progress" && i.Status != "done" {
		return ErrInvalid
	}
	if i.Priority != "low" && i.Priority != "medium" && i.Priority != "high" {
		return ErrInvalid
	}
	if i.AssigneeID != nil && *i.AssigneeID < 1 {
		return ErrInvalid
	}
	return nil
}

func CanEdit(role string) bool { return role == "owner" || role == "editor" }
