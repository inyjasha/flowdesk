package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"flowdesk/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type Repository interface {
	Register(context.Context, string, string, string) (domain.User, error)
	Credentials(context.Context, string) (domain.User, string, error)
	NewSession(context.Context, int64) (string, error)
}
type Service struct {
	repo      Repository
	dummyHash []byte
}

func New(repo Repository) (*Service, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("unused-dummy-password"), bcrypt.DefaultCost)
	return &Service{repo: repo, dummyHash: hash}, err
}
func normalize(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 {
		return "", domain.ErrInvalid
	}
	return email, nil
}
func (s *Service) Register(ctx context.Context, name, email, password string) (domain.User, string, error) {
	email, err := normalize(email)
	if err != nil {
		return domain.User{}, "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || len(password) < 10 || len(password) > 72 {
		return domain.User{}, "", domain.ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, "", err
	}
	user, err := s.repo.Register(ctx, name, email, string(hash))
	if err != nil {
		return user, "", err
	}
	token, err := s.repo.NewSession(ctx, user.ID)
	return user, token, err
}
func (s *Service) Login(ctx context.Context, email, password string) (domain.User, string, error) {
	email, err := normalize(email)
	if err != nil || len(password) > 72 {
		return domain.User{}, "", domain.ErrUnauthorized
	}
	user, hash, err := s.repo.Credentials(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return domain.User{}, "", domain.ErrUnauthorized
	}
	if err != nil {
		return user, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return domain.User{}, "", domain.ErrUnauthorized
	}
	token, err := s.repo.NewSession(ctx, user.ID)
	return user, token, err
}
