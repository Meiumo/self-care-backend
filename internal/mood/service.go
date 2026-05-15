package mood

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/romangolovachev/selfcare/internal/notification"
)

var ErrInvalidScore = errors.New("score must be between 1 and 10")

type notifier interface {
	CheckAndPushStreak(ctx context.Context, userID int64)
	CheckAndPushOverwork(ctx context.Context, userID int64, workHours float64)
}

type Service struct {
	repo     *Repository
	db       *pgxpool.Pool
	notifSvc notifier
}

func NewService(repo *Repository, db *pgxpool.Pool, notifSvc *notification.Service) *Service {
	return &Service{repo: repo, db: db, notifSvc: notifSvc}
}

// moodDelta maps UI mood level (0-4) to stress delta.
var moodStressDelta = [5]int{3, 2, 0, -1, -2}

// calculateStress loads tag weights from DB and computes stress level (1-10).
func (s *Service) calculateStress(ctx context.Context, score int, tags []string) int {
	// score 2,4,6,8,10 → level 0,1,2,3,4
	level := score/2 - 1
	if level < 0 { level = 0 }
	if level > 4 { level = 4 }

	base := 5 + moodStressDelta[level]

	if len(tags) == 0 {
		return clamp(base, 1, 10)
	}

	rows, err := s.db.Query(ctx,
		`SELECT weight FROM tag_stress_weights WHERE tag = ANY($1)`,
		tags,
	)
	if err != nil {
		return clamp(base, 1, 10)
	}
	defer rows.Close()

	for rows.Next() {
		var w int
		if err := rows.Scan(&w); err == nil {
			base += w
		}
	}
	return clamp(base, 1, 10)
}

func clamp(v, min, max int) int {
	if v < min { return min }
	if v > max { return max }
	return v
}

func (s *Service) Tags(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT tag FROM tag_stress_weights ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

type CreateInput struct {
	UserID      int64
	Score       int
	StressLevel int
	WorkHours   float64
	Note        string
	Tags        []string
}

type Entry struct {
	ID          int64     `json:"id"`
	Score       int       `json:"score"`
	StressLevel int       `json:"stress_level"`
	WorkHours   float64   `json:"work_hours"`
	Note        string    `json:"note"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Entry, error) {
	if in.Score < 1 || in.Score > 10 {
		return nil, ErrInvalidScore
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	in.StressLevel = s.calculateStress(ctx, in.Score, in.Tags)

	id, err := s.repo.Create(ctx, DBEntry{
		UserID:      in.UserID,
		Score:       in.Score,
		StressLevel: in.StressLevel,
		WorkHours:   in.WorkHours,
		Note:        in.Note,
		Tags:        in.Tags,
	})
	if err != nil {
		return nil, err
	}

	go s.notifSvc.CheckAndPushStreak(context.Background(), in.UserID)
	go s.notifSvc.CheckAndPushOverwork(context.Background(), in.UserID, in.WorkHours)

	return &Entry{
		ID:          id,
		Score:       in.Score,
		StressLevel: in.StressLevel,
		WorkHours:   in.WorkHours,
		Note:        in.Note,
		Tags:        in.Tags,
		CreatedAt:   time.Now(),
	}, nil
}

func (s *Service) Today(ctx context.Context, userID int64) (*Entry, error) {
	e, err := s.repo.FindToday(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Entry{
		ID:          e.ID,
		Score:       e.Score,
		StressLevel: e.StressLevel,
		WorkHours:   e.WorkHours,
		Note:        e.Note,
		Tags:        e.Tags,
		CreatedAt:   e.CreatedAt,
	}, nil
}

type UpdateInput struct {
	ID          int64
	UserID      int64
	Score       int
	WorkHours   float64
	StressLevel int
	Tags        []string
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*Entry, error) {
	if in.Score < 1 || in.Score > 10 {
		in.Score = 6
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}
	in.StressLevel = s.calculateStress(ctx, in.Score, in.Tags)
	if err := s.repo.Update(ctx, in.ID, in.UserID, in.Score, in.StressLevel, in.WorkHours, in.Tags); err != nil {
		return nil, err
	}

	go s.notifSvc.CheckAndPushOverwork(context.Background(), in.UserID, in.WorkHours)
	return &Entry{
		ID:          in.ID,
		Score:       in.Score,
		StressLevel: in.StressLevel,
		WorkHours:   in.WorkHours,
		Tags:        in.Tags,
		CreatedAt:   time.Now(),
	}, nil
}

func (s *Service) List(ctx context.Context, userID int64, page int) ([]Entry, error) {
	const pageSize = 20
	offset := (page - 1) * pageSize
	rows, err := s.repo.List(ctx, userID, pageSize, offset)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[i] = Entry{
			ID:          r.ID,
			Score:       r.Score,
			StressLevel: r.StressLevel,
			WorkHours:   r.WorkHours,
			Note:        r.Note,
			Tags:        r.Tags,
			CreatedAt:   r.CreatedAt,
		}
	}
	return entries, nil
}

type WeeklyReport struct {
	AvgMood      float64 `json:"avg_mood"`
	AvgStress    float64 `json:"avg_stress"`
	AvgWorkHours float64 `json:"avg_work_hours"`
	EntriesCount int     `json:"entries_count"`
}

func (s *Service) WeeklyReport(ctx context.Context, userID int64) (*WeeklyReport, error) {
	stats, err := s.repo.WeeklyStats(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &WeeklyReport{
		AvgMood:      stats.AvgMood,
		AvgStress:    stats.AvgStress,
		AvgWorkHours: stats.AvgWorkHours,
		EntriesCount: stats.EntriesCount,
	}, nil
}
