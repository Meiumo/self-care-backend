package mood

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

type DBEntry struct {
	ID         int64
	UserID     int64
	Score      int
	StressLevel int
	WorkHours  float64
	Note       string
	Tags       []string
	CreatedAt  time.Time
}

func (r *Repository) Create(ctx context.Context, e DBEntry) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO mood_entries (user_id, score, stress_level, work_hours, note, tags)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		e.UserID, e.Score, e.StressLevel, e.WorkHours, e.Note, e.Tags,
	).Scan(&id)
	return id, err
}

func (r *Repository) List(ctx context.Context, userID int64, limit, offset int) ([]DBEntry, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, score, stress_level, work_hours, note, tags, created_at
		 FROM mood_entries WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []DBEntry
	for rows.Next() {
		var e DBEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Score, &e.StressLevel, &e.WorkHours, &e.Note, &e.Tags, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (r *Repository) WeeklyStats(ctx context.Context, userID int64) (*WeeklyStats, error) {
	stats := &WeeklyStats{}
	err := r.db.QueryRow(ctx, `
		SELECT
			COALESCE(AVG(score), 0),
			COALESCE(AVG(stress_level), 0),
			COALESCE(AVG(work_hours), 0),
			COUNT(*)
		FROM mood_entries
		WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '7 days'`,
		userID,
	).Scan(&stats.AvgMood, &stats.AvgStress, &stats.AvgWorkHours, &stats.EntriesCount)
	return stats, err
}

type WeeklyStats struct {
	AvgMood      float64
	AvgStress    float64
	AvgWorkHours float64
	EntriesCount int
}
