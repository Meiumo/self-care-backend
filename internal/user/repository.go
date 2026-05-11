package user

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

type DBUser struct {
	ID        int64
	Email     string
	Name      string
	AvatarURL string
	IsPremium bool
}

func (r *Repository) FindByID(ctx context.Context, id int64) (*DBUser, error) {
	u := &DBUser{}
	err := r.db.QueryRow(ctx,
		`SELECT id, email, name, COALESCE(avatar_url, ''), is_premium FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.IsPremium)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (r *Repository) UpdateProfile(ctx context.Context, id int64, name, avatarURL string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET name = $1, avatar_url = $2, updated_at = NOW() WHERE id = $3`,
		name, avatarURL, id,
	)
	return err
}

func (r *Repository) SetPremium(ctx context.Context, id int64, premium bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET is_premium = $1, updated_at = NOW() WHERE id = $2`,
		premium, id,
	)
	return err
}
