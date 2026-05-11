package mood

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidScore = errors.New("score must be between 1 and 10")

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
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
	if in.StressLevel < 1 || in.StressLevel > 10 {
		in.StressLevel = 5
	}
	if in.Tags == nil {
		in.Tags = []string{}
	}

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
