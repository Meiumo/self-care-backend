package admin

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

type StatsRow struct {
	TotalUsers   int `json:"total_users"`
	PremiumUsers int `json:"premium_users"`
	ActiveToday  int `json:"active_today"`
}

func (r *Repository) Stats(ctx context.Context) (*StatsRow, error) {
	s := &StatsRow{}
	err := r.db.QueryRow(ctx,
		`SELECT
		    COUNT(*)                                              AS total_users,
		    COUNT(*) FILTER (WHERE is_premium)                   AS premium_users,
		    COUNT(DISTINCT id) FILTER (
		        WHERE id IN (
		            SELECT DISTINCT user_id FROM mood_entries
		            WHERE ts_to_date(created_at) = CURRENT_DATE
		        )
		    )                                                     AS active_today
		 FROM users`,
	).Scan(&s.TotalUsers, &s.PremiumUsers, &s.ActiveToday)
	return s, err
}

type UserRow struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	IsPremium bool      `json:"is_premium"`
	IsAdmin   bool      `json:"is_admin"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *Repository) ListUsers(ctx context.Context) ([]UserRow, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, email, name, is_premium, is_admin, created_at
		 FROM users ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsPremium, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (r *Repository) SetPremium(ctx context.Context, userID int64, premium bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET is_premium = $1, updated_at = NOW() WHERE id = $2`,
		premium, userID,
	)
	return err
}

func (r *Repository) SetAdmin(ctx context.Context, userID int64, admin bool) error {
	_, err := r.db.Exec(ctx,
		`UPDATE users SET is_admin = $1, updated_at = NOW() WHERE id = $2`,
		admin, userID,
	)
	return err
}

func (r *Repository) IsAdmin(ctx context.Context, userID int64) (bool, error) {
	var v bool
	err := r.db.QueryRow(ctx,
		`SELECT is_admin FROM users WHERE id = $1`, userID,
	).Scan(&v)
	return v, err
}

type ErrorEntry struct {
	ID           int64     `json:"id"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Status       int       `json:"status"`
	RequestID    *string   `json:"request_id,omitempty"`
	IP           *string   `json:"ip,omitempty"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	ErrorMessage *string   `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r *Repository) ListErrors(ctx context.Context, limit int) ([]ErrorEntry, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, method, path, status, request_id, ip, user_agent, error_message, created_at
		 FROM error_log ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ErrorEntry
	for rows.Next() {
		var e ErrorEntry
		if err := rows.Scan(
			&e.ID, &e.Method, &e.Path, &e.Status,
			&e.RequestID, &e.IP, &e.UserAgent, &e.ErrorMessage,
			&e.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteLRSession(ctx context.Context, userID int64, eventTag string) error {
	_, err := r.db.Exec(ctx,
		`DELETE FROM live_response_sessions
		 WHERE user_id = $1 AND event_tag = $2 AND ts_to_date(created_at) = CURRENT_DATE`,
		userID, eventTag,
	)
	return err
}

// CreateTestLRSession inserts a fake session (no AI call) for follow-up notification testing.
// Any existing session for the same user+tag+day is deleted first.
func (r *Repository) CreateTestLRSession(ctx context.Context, userID int64, eventTag string) (int64, error) {
	_, _ = r.db.Exec(ctx,
		`DELETE FROM live_response_sessions
		 WHERE user_id = $1 AND event_tag = $2 AND ts_to_date(created_at) = CURRENT_DATE`,
		userID, eventTag,
	)
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO live_response_sessions
		   (user_id, event_tag, event_weight, user_note, ai_message,
		    advice_tags, followup_hours, followup_question)
		 VALUES ($1, $2, 0, '', '[ТЕСТ] Тестовая сессия для проверки follow-up уведомления.', '{}', 1, 'Как ты сейчас?')
		 RETURNING id`,
		userID, eventTag,
	).Scan(&id)
	return id, err
}
