// Package personalinsights computes outcome-based personal insights once per day
// and merges them into the existing insight_reports JSONB as "personal_insights".
package personalinsights

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Insight mirrors analysis.Insight so we avoid a circular import.
type Insight struct {
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Tone  string `json:"tone"`
}

// Service recalculates personal insights for all active users daily.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

// FillNextDayMoods fills advice_outcomes.next_day_mood for rows where it is still NULL.
// Runs before ComputeAll so mood-lift insights have fresh data.
func (s *Service) FillNextDayMoods(ctx context.Context) {
	_, err := s.db.Exec(ctx,
		`UPDATE advice_outcomes ao
		 SET next_day_mood = m.score
		 FROM mood_entries m
		 WHERE ao.next_day_mood IS NULL
		   AND m.user_id = ao.user_id
		   AND DATE(m.created_at AT TIME ZONE 'UTC') =
		       DATE(ao.created_at AT TIME ZONE 'UTC') + 1`,
	)
	if err != nil {
		slog.Default().Error("personalinsights: fill next_day_mood", "err", err)
	}
}

// ComputeAll regenerates personal insights for every user active in the last 30 days.
func (s *Service) ComputeAll(ctx context.Context) {
	rows, err := s.db.Query(ctx,
		`SELECT DISTINCT user_id FROM mood_entries WHERE created_at >= NOW() - INTERVAL '30 days'`,
	)
	if err != nil {
		slog.Default().Error("personalinsights: fetch users", "err", err)
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

	slog.Default().Info("personalinsights: computing", "users", len(uids))
	for _, uid := range uids {
		if err := s.computeForUser(ctx, uid); err != nil {
			slog.Default().Error("personalinsights: user", "user_id", uid, "err", err)
		}
	}
}

// ── Internal ──────────────────────────────────────────────────────────────────

func (s *Service) computeForUser(ctx context.Context, userID int64) error {
	var insights []Insight

	if i, ok := s.weeklyTrigger(ctx, userID); ok {
		insights = append(insights, i)
	}
	if i, ok := s.adviceSuccess(ctx, userID); ok {
		insights = append(insights, i)
	}
	if i, ok := s.adviceMoodLift(ctx, userID); ok {
		insights = append(insights, i)
	}

	data, err := json.Marshal(insights)
	if err != nil {
		return err
	}

	// Merge into insight_reports: upsert and patch only the personal_insights key.
	_, err = s.db.Exec(ctx,
		`INSERT INTO insight_reports (user_id, data, generated_at)
		 VALUES ($1, jsonb_build_object('personal_insights', $2::jsonb), NOW())
		 ON CONFLICT (user_id) DO UPDATE
		 SET data = insight_reports.data ||
		            jsonb_build_object('personal_insights', EXCLUDED.data->'personal_insights'),
		     generated_at = NOW()`,
		userID, string(data),
	)
	return err
}

// ── Query 1: most frequent heavy event in the last 7 days ─────────────────────

func (s *Service) weeklyTrigger(ctx context.Context, userID int64) (Insight, bool) {
	var tag string
	var cnt int
	err := s.db.QueryRow(ctx,
		`SELECT event_tag, COUNT(*) AS cnt
		 FROM live_response_sessions
		 WHERE user_id = $1
		   AND created_at >= NOW() - INTERVAL '7 days'
		   AND event_weight >= 3
		 GROUP BY event_tag
		 ORDER BY cnt DESC
		 LIMIT 1`,
		userID,
	).Scan(&tag, &cnt)
	if err != nil || cnt < 2 {
		return Insight{}, false
	}

	days := 7
	return Insight{
		Icon:  "🎯",
		Title: fmt.Sprintf("Главный триггер недели — «%s»", tag),
		Body: fmt.Sprintf(
			"%d раза за %d дней. Это закономерность — стоит заранее готовиться к таким ситуациям.",
			cnt, days,
		),
		Tone: "warning",
	}, true
}

// ── Query 2: best advice tag by follow-up success rate ────────────────────────

func (s *Service) adviceSuccess(ctx context.Context, userID int64) (Insight, bool) {
	var tag string
	var total int
	var rate float64
	err := s.db.QueryRow(ctx,
		`SELECT advice_tag,
		        COUNT(*) AS total,
		        SUM(CASE WHEN followup_answer = 'yes' THEN 1 ELSE 0 END)::float / COUNT(*) AS rate
		 FROM advice_outcomes
		 WHERE user_id = $1 AND followup_answer IN ('yes', 'no')
		 GROUP BY advice_tag
		 HAVING COUNT(*) >= 3
		 ORDER BY rate DESC
		 LIMIT 1`,
		userID,
	).Scan(&tag, &total, &rate)
	if err != nil || rate < 0.6 {
		return Insight{}, false
	}

	pct := int(math.Round(rate * 100))
	return Insight{
		Icon:  "💚",
		Title: "Что тебе реально помогает",
		Body: fmt.Sprintf(
			"Совет «%s» помогает тебе в %d%% случаев — это выше среднего. Держи его под рукой.",
			tag, pct,
		),
		Tone: "positive",
	}, true
}

// ── Query 3: mood lift when following advice vs not ───────────────────────────

func (s *Service) adviceMoodLift(ctx context.Context, userID int64) (Insight, bool) {
	var tag, eventTag string
	var yesAvg, noAvg float64
	err := s.db.QueryRow(ctx,
		`SELECT advice_tag, event_tag,
		        ROUND(AVG(CASE WHEN followup_answer = 'yes' THEN next_day_mood END)::numeric, 1) AS yes_avg,
		        ROUND(AVG(CASE WHEN followup_answer = 'no'  THEN next_day_mood END)::numeric, 1) AS no_avg
		 FROM advice_outcomes
		 WHERE user_id = $1 AND next_day_mood IS NOT NULL
		   AND followup_answer IN ('yes', 'no')
		 GROUP BY advice_tag, event_tag
		 HAVING COUNT(CASE WHEN followup_answer = 'yes' THEN 1 END) >= 2
		    AND COUNT(CASE WHEN followup_answer = 'no'  THEN 1 END) >= 2
		 ORDER BY (AVG(CASE WHEN followup_answer = 'yes' THEN next_day_mood END) -
		           AVG(CASE WHEN followup_answer = 'no'  THEN next_day_mood END)) DESC NULLS LAST
		 LIMIT 1`,
		userID,
	).Scan(&tag, &eventTag, &yesAvg, &noAvg)
	if err != nil || noAvg == 0 || yesAvg <= noAvg {
		return Insight{}, false
	}

	deltaPct := int(math.Round((yesAvg - noAvg) / noAvg * 100))
	if deltaPct < 5 {
		return Insight{}, false
	}
	return Insight{
		Icon:  "📈",
		Title: fmt.Sprintf("Совет после «%s» работает", eventTag),
		Body: fmt.Sprintf(
			"Когда ты следуешь совету, настроение на следующий день в среднем на %d%% выше, чем когда не следуешь.",
			deltaPct,
		),
		Tone: "positive",
	}, true
}
