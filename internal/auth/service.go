package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
)

var (
	ErrEmailTaken    = errors.New("email already registered")
	ErrInvalidCreds  = errors.New("invalid email or password")
	ErrWeakPassword  = errors.New("password must be at least 8 characters")
	ErrInvalidEmail  = errors.New("invalid email")
)

type authRepository interface {
	CreateUser(ctx context.Context, email, passwordHash, name string) (int64, error)
	FindByEmail(ctx context.Context, email string) (*DBUser, error)
	BumpTokenVersion(ctx context.Context, userID int64) (int, error)
	GetTokenVersion(ctx context.Context, userID int64) (int, error)
}

type Service struct {
	repo authRepository
	jwt  *jwtutil.Service
}

func NewService(repo *Repository, jwt *jwtutil.Service) *Service {
	return &Service{repo: repo, jwt: jwt}
}

type RegisterInput struct {
	Email    string
	Password string
	Name     string
}

type AuthResult struct {
	Token     string `json:"token"`
	UserID    int64  `json:"user_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	IsPremium bool   `json:"is_premium"`
}

func (s *Service) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if !strings.Contains(in.Email, "@") {
		return nil, ErrInvalidEmail
	}
	if len(in.Password) < 8 {
		return nil, ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	id, err := s.repo.CreateUser(ctx, in.Email, string(hash), in.Name)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	// New users start at token_version = 1 (DB default).
	token, err := s.jwt.Sign(id, 1)
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: token, UserID: id, Email: in.Email, Name: in.Name}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := s.repo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCreds
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCreds
	}

	token, err := s.jwt.Sign(u.ID, u.TokenVersion)
	if err != nil {
		return nil, err
	}
	return &AuthResult{Token: token, UserID: u.ID, Email: u.Email, Name: u.Name, IsPremium: u.IsPremium}, nil
}

// Logout invalidates all existing tokens for the user by bumping token_version.
func (s *Service) Logout(ctx context.Context, userID int64) error {
	_, err := s.repo.BumpTokenVersion(ctx, userID)
	return err
}

// VersionChecker returns a jwtutil.VersionChecker that validates token_version against the DB.
func (s *Service) VersionChecker() func(ctx context.Context, userID int64, tokenVersion int) bool {
	return func(ctx context.Context, userID int64, tokenVersion int) bool {
		current, err := s.repo.GetTokenVersion(ctx, userID)
		if err != nil {
			return false
		}
		return tokenVersion == current
	}
}
