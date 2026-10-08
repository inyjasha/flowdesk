package auth

import (
	"context"
	"errors"
	"testing"

	"flowdesk/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type fakeRepo struct {
	savedEmail, savedHash string
	called                bool
}

func (r *fakeRepo) Register(_ context.Context, name, email, hash string) (domain.User, error) {
	r.called = true
	r.savedEmail = email
	r.savedHash = hash
	return domain.User{ID: 1, Name: name, Email: email}, nil
}
func (r *fakeRepo) Credentials(context.Context, string) (domain.User, string, error) {
	return domain.User{ID: 1}, r.savedHash, nil
}
func (r *fakeRepo) NewSession(context.Context, int64) (string, error) { return "test-session", nil }
func TestRegisterAndLogin(t *testing.T) {
	repo := &fakeRepo{}
	service, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Register(context.Background(), "Анна", "Anna@Example.com", "short")
	if !errors.Is(err, domain.ErrInvalid) || repo.called {
		t.Fatal("короткий пароль дошёл до хранилища")
	}
	user, token, err := service.Register(context.Background(), "Анна", " Anna@Example.com ", "long-password-123")
	if err != nil || token == "" || user.Email != "anna@example.com" {
		t.Fatalf("%+v %q %v", user, token, err)
	}
	if repo.savedHash == "long-password-123" || bcrypt.CompareHashAndPassword([]byte(repo.savedHash), []byte("long-password-123")) != nil {
		t.Fatal("пароль должен храниться в виде bcrypt-хеша")
	}
	if _, _, err = service.Login(context.Background(), "anna@example.com", "wrong-password"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("неверный пароль принят", err)
	}
	if _, _, err = service.Login(context.Background(), "anna@example.com", "long-password-123"); err != nil {
		t.Fatal(err)
	}
}
