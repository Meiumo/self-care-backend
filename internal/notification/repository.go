package notification

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

type DBNotification struct {
	ID        int64
	UserID    int64
	Type      string
	Title     string
	Body      string
	IsRead    bool
	CreatedAt time.Time
}

func (r *Repository) Create(ctx context.Context, userID int64, nType, title, body string) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO notifications (user_id, type, title, body)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, nType, title, body,
	).Scan(&id)
	return id, err
}

func (r *Repository) List(ctx context.Context, userID int64, limit int) ([]DBNotification, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, type, title, body, is_read, created_at
		 FROM notifications
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBNotification
	for rows.Next() {
		var n DBNotification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repository) MarkRead(ctx context.Context, id, userID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE notifications SET is_read = TRUE WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	return err
}

func (r *Repository) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE notifications SET is_read = TRUE WHERE user_id = $1`,
		userID,
	)
	return err
}

func (r *Repository) UnreadCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND is_read = FALSE`,
		userID,
	).Scan(&count)
	return count, err
}

// sentToday checks whether a notification of a given type was already created today for the user.
func (r *Repository) sentToday(ctx context.Context, userID int64, nType string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM notifications
			WHERE user_id = $1 AND type = $2 AND ts_to_date(created_at) = CURRENT_DATE
		)`,
		userID, nType,
	).Scan(&exists)
	return exists, err
}

// SendDailyReminders bulk-inserts a mood_reminder for every user who
// has not logged mood today and has not yet received a reminder today.
func (r *Repository) SendDailyReminders(ctx context.Context) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO notifications (user_id, type, title, body)
		 SELECT u.id,
		        'mood_reminder',
		        'Время отметить настроение',
		        'Ты ещё не отмечался сегодня — это займёт меньше минуты.'
		 FROM users u
		 WHERE u.id NOT IN (
		     SELECT DISTINCT user_id FROM mood_entries
		     WHERE ts_to_date(created_at) = CURRENT_DATE
		 )
		 AND u.id NOT IN (
		     SELECT DISTINCT user_id FROM notifications
		     WHERE type = 'mood_reminder' AND ts_to_date(created_at) = CURRENT_DATE
		 )`,
	)
	return err
}
