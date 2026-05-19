package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
)

type mockAuthRepo struct {
	createUserFn  func(ctx context.Context, email, hash, name string) (int64, error)
	findByEmailFn func(ctx context.Context, email string) (*DBUser, error)
	bumpVersionFn func(ctx context.Context, userID int64) (int, error)
	getVersionFn  func(ctx context.Context, userID int64) (int, error)
}

func (m *mockAuthRepo) CreateUser(ctx context.Context, email, hash, name string) (int64, error) {
	return m.createUserFn(ctx, email, hash, name)
}
func (m *mockAuthRepo) FindByEmail(ctx context.Context, email string) (*DBUser, error) {
	return m.findByEmailFn(ctx, email)
}
func (m *mockAuthRepo) BumpTokenVersion(ctx context.Context, userID int64) (int, error) {
	return m.bumpVersionFn(ctx, userID)
}
func (m *mockAuthRepo) GetTokenVersion(ctx context.Context, userID int64) (int, error) {
	return m.getVersionFn(ctx, userID)
}

func newMockService(repo authRepository) *Service {
	return &Service{repo: repo, jwt: jwtutil.New("test-secret-key-32-chars-padded!!")}
}

func TestRegister_WeakPassword(t *testing.T) {
	svc := newMockService(&mockAuthRepo{})
	_, err := svc.Register(context.Background(), RegisterInput{
		Email: "user@example.com", Password: "short", Name: "Test",
	})
	assert.ErrorIs(t, err, ErrWeakPassword)
}

func TestRegister_InvalidEmail(t *testing.T) {
	svc := newMockService(&mockAuthRepo{})
	_, err := svc.Register(context.Background(), RegisterInput{
		Email: "notanemail", Password: "strongpassword", Name: "Test",
	})
	assert.ErrorIs(t, err, ErrInvalidEmail)
}

func TestRegister_EmailTaken(t *testing.T) {
	repo := &mockAuthRepo{
		createUserFn: func(_ context.Context, _, _, _ string) (int64, error) {
			return 0, errors.New("unique constraint violation")
		},
	}
	_, err := newMockService(repo).Register(context.Background(), RegisterInput{
		Email: "taken@example.com", Password: "strongpassword", Name: "Test",
	})
	assert.ErrorIs(t, err, ErrEmailTaken)
}

func TestRegister_Success(t *testing.T) {
	repo := &mockAuthRepo{
		createUserFn: func(_ context.Context, _, _, _ string) (int64, error) {
			return 99, nil
		},
	}
	result, err := newMockService(repo).Register(context.Background(), RegisterInput{
		Email: "new@example.com", Password: "strongpassword", Name: "Alice",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(99), result.UserID)
	assert.NotEmpty(t, result.Token)
	assert.Equal(t, "new@example.com", result.Email)
}

func TestLogin_UserNotFound(t *testing.T) {
	repo := &mockAuthRepo{
		findByEmailFn: func(_ context.Context, _ string) (*DBUser, error) {
			return nil, pgx.ErrNoRows
		},
	}
	_, err := newMockService(repo).Login(context.Background(), "nobody@example.com", "password")
	assert.ErrorIs(t, err, ErrInvalidCreds)
}

func TestLogin_WrongPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	repo := &mockAuthRepo{
		findByEmailFn: func(_ context.Context, _ string) (*DBUser, error) {
			return &DBUser{ID: 1, Email: "u@e.com", PasswordHash: string(hash), TokenVersion: 1}, nil
		},
	}
	_, err := newMockService(repo).Login(context.Background(), "u@e.com", "wrongpassword")
	assert.ErrorIs(t, err, ErrInvalidCreds)
}

func TestLogin_Success(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	repo := &mockAuthRepo{
		findByEmailFn: func(_ context.Context, _ string) (*DBUser, error) {
			return &DBUser{ID: 7, Email: "u@e.com", PasswordHash: string(hash), Name: "Bob", TokenVersion: 2}, nil
		},
	}
	result, err := newMockService(repo).Login(context.Background(), "u@e.com", "correctpassword")
	require.NoError(t, err)
	assert.Equal(t, int64(7), result.UserID)
	assert.NotEmpty(t, result.Token)
}
