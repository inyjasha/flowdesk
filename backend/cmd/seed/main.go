package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"flowdesk/internal/domain"
	"flowdesk/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	store := &postgres.Store{Pool: pool}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	if _, _, err = store.Credentials(ctx, "anna@flowdesk.local"); err == nil {
		fmt.Println("Демо-аккаунт уже есть. Его задачи не изменены.")
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	password := os.Getenv("DEMO_PASSWORD")
	if len(password) < 10 || len(password) > 72 {
		return fmt.Errorf("задай DEMO_PASSWORD длиной 10–72 байта")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	user, err := store.Register(ctx, "Анна", "anna@flowdesk.local", string(hash))
	if err != nil {
		return err
	}
	workspace, err := store.CreateWorkspace(ctx, user.ID, "Product Studio")
	if err != nil {
		return err
	}
	tasks := []struct {
		title, description, status, priority string
		days                                 int
	}{
		{"Собрать обратную связь", "Поговорить с первыми пользователями и выделить повторяющиеся запросы.", "todo", "high", 2},
		{"Продумать первый запуск", "Показать команде путь от регистрации до первой завершённой задачи.", "todo", "medium", 4},
		{"Описать сценарии приглашений", "Проверить роли и понятность одноразового кода для нового участника.", "todo", "low", 6},
		{"Обновить доску задач", "Добавить спокойные цвета, быстрые фильтры и читаемые карточки.", "in_progress", "high", 1},
		{"Подготовить пользовательские тесты", "Пройти основные сценарии с командой и записать замечания.", "in_progress", "medium", 3},
		{"Согласовать план релиза", "Определить, что войдёт в первую версию, а что останется на следующий цикл.", "in_progress", "low", 5},
		{"Настроить рабочее пространство", "Договориться о статусах и приоритетах задач.", "done", "medium", -1},
		{"Собрать прототип интерфейса", "Проверить структуру доски и расположение основных действий.", "done", "low", -2},
		{"Определить цель команды", "Записать ожидаемый результат и критерии готовности.", "done", "high", -3},
	}
	for _, item := range tasks {
		due := time.Now().AddDate(0, 0, item.days)
		in := domain.TaskInput{Title: item.title, Description: item.description, Status: item.status, Priority: item.priority, AssigneeID: &user.ID, DueDate: &due}
		if _, err = store.SaveTask(ctx, workspace.ID, user.ID, 0, in); err != nil {
			return err
		}
	}
	fmt.Println("Демо готово. Вход: anna@flowdesk.local. Пароль — значение DEMO_PASSWORD.")
	return nil
}
