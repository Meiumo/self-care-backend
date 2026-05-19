// Package weeklycard generates a weekly summary card for each active user.
package weeklycard

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Card holds the data shown in the weekly summary UI.
type Card struct {
	WeekStart    string  `json:"week_start"`     // "2006-01-02"
	TopEvent     string  `json:"top_event"`      // most frequent heavy event tag
	TopEventCnt  int     `json:"top_event_count"`
	StressDelta  float64 `json:"stress_delta"`   // avg stress this week minus last week
	BestAdvice   string  `json:"best_advice"`    // advice_tag with highest yes-rate this week
	PersonalNote string  `json:"personal_note"`  // first personal insight body, if any
}

// Service computes and persists weekly cards.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// ComputeAll regenerates cards for all users active in the last 7 days.
func (s *Service) ComputeAll(ctx context.Context) {
	rows, err := s.db.Query(ctx,
		`SELECT DISTINCT user_id FROM mood_entries WHERE created_at >= NOW() - INTERVAL '7 days'`,
	)
	if err != nil {
		slog.Default().Error("weeklycard: fetch users", "err", err)
		return
	}
	defer rows.Close()

	var uids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			uids = append(uids, id)
		}
	}
	_ = rows.Err()

	slog.Default().Info("weeklycard: computing", "users", len(uids))
	for _, uid := range uids {
		if err := s.ComputeForUser(ctx, uid); err != nil {
			slog.Default().Error("weeklycard: user", "user_id", uid, "err", err)
		}
	}
}

// GetCard returns the latest weekly card for a user, or nil if none exists.
func (s *Service) GetCard(ctx context.Context, userID int64) (*Card, error) {
	var raw []byte
	var weekStart time.Time
	err := s.db.QueryRow(ctx,
		`SELECT data, week_start FROM weekly_cards WHERE user_id = $1`,
		userID,
	).Scan(&raw, &weekStart)
	if err != nil {
		return nil, err
	}

	var card Card
	if err := json.Unmarshal(raw, &card); err != nil {
		return nil, err
	}
	card.WeekStart = weekStart.Format("2006-01-02")
	return &card, nil
}

// ── Internal ──────────────────────────────────────────────────────────────────

func (s *Service) ComputeForUser(ctx context.Context, userID int64) error {
	card := Card{}

	// Top heavy event this week
	_ = s.db.QueryRow(ctx,
		`SELECT event_tag, COUNT(*) AS cnt
		 FROM live_response_sessions
		 WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '7 days'
		   AND event_weight >= 2
		 GROUP BY event_tag ORDER BY cnt DESC LIMIT 1`,
		userID,
	).Scan(&card.TopEvent, &card.TopEventCnt)

	// Stress delta: avg this week vs previous week
	var thisWeek, lastWeek *float64
	_ = s.db.QueryRow(ctx,
		`SELECT
		    AVG(CASE WHEN created_at >= NOW() - INTERVAL '7 days' THEN stress_level END),
		    AVG(CASE WHEN created_at >= NOW() - INTERVAL '14 days'
		             AND created_at <  NOW() - INTERVAL '7 days'  THEN stress_level END)
		 FROM mood_entries WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '14 days'`,
		userID,
	).Scan(&thisWeek, &lastWeek)
	if thisWeek != nil && lastWeek != nil && *lastWeek != 0 {
		card.StressDelta = roundOne(*thisWeek - *lastWeek)
	}

	// Best advice this week (highest yes-rate, ≥2 answers)
	_ = s.db.QueryRow(ctx,
		`SELECT advice_tag
		 FROM advice_outcomes
		 WHERE user_id = $1
		   AND created_at >= NOW() - INTERVAL '7 days'
		   AND followup_answer IN ('yes', 'no')
		 GROUP BY advice_tag
		 HAVING COUNT(*) >= 2
		 ORDER BY SUM(CASE WHEN followup_answer = 'yes' THEN 1 ELSE 0 END)::float / COUNT(*) DESC
		 LIMIT 1`,
		userID,
	).Scan(&card.BestAdvice)

	// First personal insight body from insight_reports
	var reportData []byte
	if err := s.db.QueryRow(ctx,
		`SELECT data FROM insight_reports WHERE user_id = $1`, userID,
	).Scan(&reportData); err == nil && len(reportData) > 0 {
		var report struct {
			PersonalInsights []struct {
				Body string `json:"body"`
			} `json:"personal_insights"`
		}
		if json.Unmarshal(reportData, &report) == nil && len(report.PersonalInsights) > 0 {
			card.PersonalNote = report.PersonalInsights[0].Body
		}
	}

	data, err := json.Marshal(card)
	if err != nil {
		return err
	}

	weekStart := mondayOf(time.Now())
	_, err = s.db.Exec(ctx,
		`INSERT INTO weekly_cards (user_id, data, week_start, generated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id) DO UPDATE
		 SET data = EXCLUDED.data, week_start = EXCLUDED.week_start, generated_at = NOW()`,
		userID, data, weekStart,
	)
	return err
}

func mondayOf(t time.Time) time.Time {
	d := int(t.Weekday())
	if d == 0 {
		d = 7
	}
	return t.AddDate(0, 0, -(d - 1)).Truncate(24 * time.Hour)
}

func roundOne(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
