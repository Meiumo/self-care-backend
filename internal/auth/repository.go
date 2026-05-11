package auth

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateUser(ctx context.Context, email, passwordHash, name string) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3) RETURNING id`,
		email, passwordHash, name,
	).Scan(&id)
	return id, err
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*DBUser, error) {
	u := &DBUser{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, password_hash, name, is_premium FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.IsPremium)
	if err != nil {
		return nil, err
	}
	return u, nil
}

type DBUser struct {
	ID           int64
	Email        string
	PasswordHash string
	Name         string
	IsPremium    bool
}
