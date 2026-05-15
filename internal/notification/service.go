package notification

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	TypeMoodReminder  = "mood_reminder"
	TypeInsightReady  = "insight_ready"
	TypeOverworkAlert = "overwork_alert"
	TypeStreak        = "streak"
)

type Notification struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

type Service struct {
	repo *Repository
	db   *pgxpool.Pool // for cross-domain queries (streak, overwork dedup)
}

func NewService(db *pgxpool.Pool, repo *Repository) *Service {
	return &Service{repo: repo, db: db}
}

func (s *Service) Push(ctx context.Context, userID int64, nType, title, body string) error {
	_, err := s.repo.Create(ctx, userID, nType, title, body)
	return err
}

func (s *Service) List(ctx context.Context, userID int64) ([]Notification, error) {
	rows, err := s.repo.List(ctx, userID, 50)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, len(rows))
	for i, r := range rows {
		out[i] = Notification{
			ID: r.ID, Type: r.Type, Title: r.Title,
			Body: r.Body, IsRead: r.IsRead, CreatedAt: r.CreatedAt,
		}
	}
	return out, nil
}

func (s *Service) MarkRead(ctx context.Context, id, userID int64) error {
	return s.repo.MarkRead(ctx, id, userID)
}

func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	return s.repo.MarkAllRead(ctx, userID)
}

func (s *Service) UnreadCount(ctx context.Context, userID int64) (int, error) {
	return s.repo.UnreadCount(ctx, userID)
}

// SendDailyReminders is called by cron at 21:00 every day.
func (s *Service) SendDailyReminders(ctx context.Context) {
	if err := s.repo.SendDailyReminders(ctx); err != nil {
		slog.Default().Error("notifications: daily reminder", "err", err)
	}
}

// CheckAndPushStreak checks the current streak for a user after a mood save
// and pushes a streak notification at milestones (3, 7, 14, 30 days).
func (s *Service) CheckAndPushStreak(ctx context.Context, userID int64) {
	streak, err := s.currentStreak(ctx, userID)
	if err != nil || streak == 0 {
		return
	}

	milestones := map[int]string{
		3:  "3 дня подряд — хорошее начало",
		7:  "Неделя подряд — ты молодец",
		14: "Две недели — привычка формируется",
		30: "30 дней — это уже часть жизни",
	}
	title, ok := milestones[streak]
	if !ok {
		return
	}

	// Don't send the same milestone twice
	sent, err := s.repo.sentToday(ctx, userID, TypeStreak)
	if err != nil || sent {
		return
	}

	body := map[int]string{
		3:  "Маленькие привычки складываются — продолжай отмечать каждый день.",
		7:  "Целая неделя отметок настроения. Паттерны уже начинают проявляться.",
		14: "14 дней подряд. Уже виден твой персональный ритм.",
		30: "30 дней отметок — поздравляем! Это серьёзная база для понимания себя.",
	}[streak]

	_ = s.Push(ctx, userID, TypeStreak, title, body)
}

// CheckAndPushOverwork pushes an overwork_alert if work hours exceed 9
// and no such notification has been sent today.
func (s *Service) CheckAndPushOverwork(ctx context.Context, userID int64, workHours float64) {
	if workHours <= 9 {
		return
	}
	sent, err := s.repo.sentToday(ctx, userID, TypeOverworkAlert)
	if err != nil || sent {
		return
	}
	_ = s.Push(ctx, userID, TypeOverworkAlert,
		"Долгий рабочий день",
		"Сегодня больше 9 часов работы. После переработки тело нуждается в 8+ часах сна — запланируй ранний отбой.",
	)
}

// PushInsightReady notifies each user that the weekly insight report is ready.
func (s *Service) PushInsightReady(ctx context.Context, userIDs []int64) {
	for _, uid := range userIDs {
		sent, err := s.repo.sentToday(ctx, uid, TypeInsightReady)
		if err != nil || sent {
			continue
		}
		_ = s.Push(ctx, uid, TypeInsightReady,
			"Твой недельный отчёт готов",
			"Мы проанализировали неделю и нашли паттерны. Посмотри инсайты в отчёте.",
		)
	}
}

// currentStreak returns how many consecutive days ending today the user has logged mood.
func (s *Service) currentStreak(ctx context.Context, userID int64) (int, error) {
	rows, err := s.db.Query(ctx,
		`SELECT DISTINCT ts_to_date(created_at)
		 FROM mood_entries
		 WHERE user_id = $1 AND ts_to_date(created_at) >= CURRENT_DATE - 31
		 ORDER BY 1 DESC`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	streak := 0
	expect := time.Now().UTC().Truncate(24 * time.Hour)

	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		d = d.UTC().Truncate(24 * time.Hour)
		if !d.Equal(expect) {
			break
		}
		streak++
		expect = expect.AddDate(0, 0, -1)
	}
	return streak, rows.Err()
}
