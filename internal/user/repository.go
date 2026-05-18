package user

import (
	"context"
	"time"

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

type Stats struct {
	StreakDays   int `json:"streak_days"`
	TotalEntries int `json:"total_entries"`
	TotalEvents  int `json:"total_events"`
}

func (r *Repository) GetStats(ctx context.Context, userID int64) (*Stats, error) {
	var totalEntries, totalEvents int
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM mood_entries WHERE user_id = $1`, userID,
	).Scan(&totalEntries); err != nil {
		return nil, err
	}
	// Total distinct event tags logged across all mood entries
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM (SELECT unnest(tags) FROM mood_entries WHERE user_id = $1) t`,
		userID,
	).Scan(&totalEvents); err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx,
		`SELECT DISTINCT ts_to_date(created_at)
		 FROM mood_entries
		 WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '31 days'
		 ORDER BY 1 DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	streak := 0
	today := time.Now().UTC().Truncate(24 * time.Hour)
	cursor := today
	for _, d := range dates {
		day := d.UTC().Truncate(24 * time.Hour)
		if day.Equal(cursor) {
			streak++
			cursor = cursor.AddDate(0, 0, -1)
		} else {
			break
		}
	}

	return &Stats{StreakDays: streak, TotalEntries: totalEntries, TotalEvents: totalEvents}, nil
}
