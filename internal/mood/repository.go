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
	ID          int64
	UserID      int64
	Score       int
	StressLevel int
	WorkHours   float64
	Note        string
	Tags        []string
	CreatedAt   time.Time
}

// Create upserts today's entry — one row per user per calendar day.
func (r *Repository) Create(ctx context.Context, e DBEntry) (int64, error) {
	var id int64
	err := r.db.QueryRow(ctx,
		`INSERT INTO mood_entries (user_id, score, stress_level, work_hours, note, tags)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, ts_to_date(created_at))
		 DO UPDATE SET
		     score        = EXCLUDED.score,
		     stress_level = EXCLUDED.stress_level,
		     work_hours   = EXCLUDED.work_hours,
		     note         = EXCLUDED.note,
		     tags         = EXCLUDED.tags
		 RETURNING id`,
		e.UserID, e.Score, e.StressLevel, e.WorkHours, e.Note, e.Tags,
	).Scan(&id)
	return id, err
}

func (r *Repository) FindToday(ctx context.Context, userID int64) (*DBEntry, error) {
	e := &DBEntry{}
	err := r.db.QueryRow(ctx,
		`SELECT id, user_id, score, stress_level, work_hours, note, tags, created_at
		 FROM mood_entries
		 WHERE user_id = $1 AND ts_to_date(created_at) = CURRENT_DATE
		 ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&e.ID, &e.UserID, &e.Score, &e.StressLevel, &e.WorkHours, &e.Note, &e.Tags, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (r *Repository) Update(ctx context.Context, id, userID int64, score, stressLevel int, workHours float64, tags []string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE mood_entries
		 SET score=$1, stress_level=$2, work_hours=$3, tags=$4
		 WHERE id=$5 AND user_id=$6`,
		score, stressLevel, workHours, tags, id, userID,
	)
	return err
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
