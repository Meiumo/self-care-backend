package analysis

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romangolovachev/selfcare/internal/notification"
)

type notifier interface {
	PushInsightReady(ctx context.Context, userIDs []int64)
}

type Service struct {
	db       *pgxpool.Pool
	notifSvc notifier
}

func NewService(db *pgxpool.Pool, notifSvc *notification.Service) *Service {
	return &Service{db: db, notifSvc: notifSvc}
}

// GetInsights returns the stored weekly report for the user.
// Returns a placeholder if no report has been generated yet.
func (s *Service) GetInsights(ctx context.Context, userID int64) (*InsightReport, error) {
	var raw []byte
	err := s.db.QueryRow(ctx,
		`SELECT data FROM insight_reports WHERE user_id = $1`,
		userID,
	).Scan(&raw)
	if err != nil {
		return &InsightReport{
			Insights: []Insight{{
				Icon:  "📅",
				Title: "Первый отчёт в пятницу",
				Body:  "Инсайты считаются каждую пятницу на основе твоих отметок за неделю. Продолжай заполнять — скоро появится полная картина.",
				Tone:  "neutral",
			}},
			Advice: []Advice{},
		}, nil
	}

	var report InsightReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

// RegenerateAll recomputes and saves insight reports for every user
// who has mood entries in the last 30 days.
func (s *Service) RegenerateAll(ctx context.Context) {
	tpl, err := s.loadTemplates(ctx)
	if err != nil {
		slog.Default().Error("insights: load templates", "err", err)
		return
	}

	rows, err := s.db.Query(ctx,
		`SELECT DISTINCT user_id FROM mood_entries WHERE created_at >= NOW() - INTERVAL '30 days'`,
	)
	if err != nil {
		slog.Default().Error("insights: fetch users", "err", err)
		return
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			userIDs = append(userIDs, id)
		}
	}
	_ = rows.Err()

	slog.Default().Info("insights: regenerating", "users", len(userIDs))
	var succeeded []int64
	for _, uid := range userIDs {
		if err := s.computeAndSave(ctx, uid, tpl); err != nil {
			slog.Default().Error("insights: regen user", "user_id", uid, "err", err)
		} else {
			succeeded = append(succeeded, uid)
		}
	}
	if len(succeeded) > 0 {
		s.notifSvc.PushInsightReady(ctx, succeeded)
	}
}

type HistoryItem struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
}

// History returns analysis_results rows for the user (newest first, limit 50).
func (s *Service) History(ctx context.Context, userID int64) ([]HistoryItem, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, created_at FROM analysis_results WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []HistoryItem
	for rows.Next() {
		var it HistoryItem
		if err := rows.Scan(&it.ID, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	if items == nil {
		items = []HistoryItem{}
	}
	return items, rows.Err()
}

// RegenerateForUser recomputes and saves the insight report for one user.
func (s *Service) RegenerateForUser(ctx context.Context, userID int64) error {
	tpl, err := s.loadTemplates(ctx)
	if err != nil {
		return err
	}
	return s.computeAndSave(ctx, userID, tpl)
}

func (s *Service) loadTemplates(ctx context.Context) (Templates, error) {
	tpl := Templates{
		Insights: make(map[string]InsightTemplate),
		Advice:   make(map[string]string),
	}

	iRows, err := s.db.Query(ctx, `SELECT rule, icon, title, body, tone FROM insight_templates`)
	if err != nil {
		return tpl, err
	}
	defer iRows.Close()
	for iRows.Next() {
		var rule string
		var t InsightTemplate
		if err := iRows.Scan(&rule, &t.Icon, &t.Title, &t.Body, &t.Tone); err != nil {
			return tpl, err
		}
		tpl.Insights[rule] = t
	}
	if err := iRows.Err(); err != nil {
		return tpl, err
	}

	aRows, err := s.db.Query(ctx, `SELECT tag, text FROM advice_templates`)
	if err != nil {
		return tpl, err
	}
	defer aRows.Close()
	for aRows.Next() {
		var tag, text string
		if err := aRows.Scan(&tag, &text); err != nil {
			return tpl, err
		}
		tpl.Advice[tag] = text
	}
	return tpl, aRows.Err()
}

func (s *Service) computeAndSave(ctx context.Context, userID int64, tpl Templates) error {
	rows, err := s.db.Query(ctx,
		`SELECT score, stress_level, work_hours, tags, created_at
		 FROM mood_entries
		 WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '30 days'
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var entries []entryData
	for rows.Next() {
		var e entryData
		if err := rows.Scan(&e.Score, &e.StressLevel, &e.WorkHours, &e.Tags, &e.CreatedAt); err != nil {
			return err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	report := computeInsights(entries, tpl)

	data, err := json.Marshal(report)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(ctx,
		`INSERT INTO insight_reports (user_id, data, generated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (user_id) DO UPDATE SET data = EXCLUDED.data, generated_at = NOW()`,
		userID, data,
	)
	return err
}
