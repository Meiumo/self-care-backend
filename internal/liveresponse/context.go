package liveresponse

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ContextBuilder assembles the dynamic context prompt based on user history depth.
type ContextBuilder struct {
	db *pgxpool.Pool
}

func NewContextBuilder(db *pgxpool.Pool) *ContextBuilder {
	return &ContextBuilder{db: db}
}

// Build returns the context prompt string for the given user.
// Returns empty string if there is no data — the system prompt is still valid.
// Called for both Generate and Chat so context is always fresh.
func (b *ContextBuilder) Build(ctx context.Context, userID int64) (string, error) {
	base, err := b.fetchBase(ctx, userID)
	if err != nil || base.totalEntries == 0 {
		return "", err
	}

	days := base.daysActive
	var parts []string

	// ── Stage 0 (always): today's mood, stress, hours and events ─────────────
	if today, err := b.fetchToday(ctx, userID); err == nil && today != "" {
		parts = append(parts, today)
	}

	// Base stats header
	parts = append(parts, fmt.Sprintf(
		"Контекст: %d отметок за %d дней, средний балл настроения %.1f/10.",
		base.totalEntries, days, base.avgScore,
	))

	// ── Stage 0.5 (3+ days): 7-day stress and mood trend ─────────────────────
	if days >= 3 {
		if trend, err := b.fetchTrend(ctx, userID); err == nil && trend != "" {
			parts = append(parts, trend)
		}
	}

	// ── Stage 2 (4+ days): most frequent event tags ───────────────────────────
	if days >= 4 {
		tags, err := b.fetchTopTags(ctx, userID, 3)
		if err == nil && len(tags) > 0 {
			parts = append(parts, "Частые события: "+strings.Join(tags, ", ")+".")
		}
	}

	// ── Stage 3 (8+ days): advice that worked ────────────────────────────────
	if days >= 8 {
		if effective, err := b.fetchEffectiveAdvice(ctx, userID); err == nil && effective != "" {
			parts = append(parts, "Советы, которые помогли ранее: "+effective+".")
		}
	}

	// ── Stage 3.5 (7+ days): weekly AI insights ──────────────────────────────
	if days >= 7 {
		if insights, err := b.fetchWeeklyInsights(ctx, userID); err == nil && insights != "" {
			parts = append(parts, insights)
		}
	}

	// ── Stage 3.7 (5+ days): recent LR sessions with outcomes ────────────────
	if days >= 5 {
		if sessions, err := b.fetchRecentSessions(ctx, userID); err == nil && sessions != "" {
			parts = append(parts, sessions)
		}
	}

	// ── Stage 4 (30+ days): mood–event correlations ───────────────────────────
	if days >= 30 {
		if corr, err := b.fetchCorrelations(ctx, userID); err == nil && corr != "" {
			parts = append(parts, "Паттерны: "+corr+".")
		}
	}

	return strings.Join(parts, " "), nil
}

// ── Internal structs ──────────────────────────────────────────────────────────

type baseStats struct {
	totalEntries int
	daysActive   int
	avgScore     float64
}

// ── Stage 0: today ────────────────────────────────────────────────────────────

func (b *ContextBuilder) fetchToday(ctx context.Context, userID int64) (string, error) {
	var score, stress, workHours int
	var tags []string
	err := b.db.QueryRow(ctx,
		`SELECT score, stress_level, work_hours, tags
		 FROM mood_entries
		 WHERE user_id = $1 AND DATE(created_at AT TIME ZONE 'UTC') = CURRENT_DATE
		 ORDER BY created_at DESC LIMIT 1`,
		userID,
	).Scan(&score, &stress, &workHours, &tags)
	if err != nil {
		return "", nil // no entry today — non-fatal
	}

	sb := fmt.Sprintf("Сегодня: настроение %d/10, стресс %d/10", score, stress)
	if workHours > 0 {
		sb += fmt.Sprintf(", %d рабочих ч.", workHours)
	}
	if len(tags) > 0 {
		sb += ". События дня: " + strings.Join(tags, ", ") + "."
	} else {
		sb += "."
	}
	return sb, nil
}

// ── Stage 0.5: 7-day trend ───────────────────────────────────────────────────

func (b *ContextBuilder) fetchTrend(ctx context.Context, userID int64) (string, error) {
	rows, err := b.db.Query(ctx,
		`SELECT AVG(stress_level)::float8, AVG(score)::float8
		 FROM mood_entries
		 WHERE user_id = $1 AND created_at >= NOW() - INTERVAL '7 days'
		 GROUP BY DATE(created_at AT TIME ZONE 'UTC')
		 ORDER BY DATE(created_at AT TIME ZONE 'UTC')`,
		userID,
	)
	if err != nil {
		return "", nil
	}
	defer rows.Close()

	var stresses, moods []float64
	for rows.Next() {
		var s, m float64
		if err := rows.Scan(&s, &m); err == nil {
			stresses = append(stresses, s)
			moods = append(moods, m)
		}
	}
	if err := rows.Err(); err != nil || len(stresses) < 2 {
		return "", nil
	}

	// Compare first half vs second half to get direction
	mid := len(stresses) / 2
	var earlyStress, lateStress, earlyMood, lateMood float64
	for i := 0; i < mid; i++ {
		earlyStress += stresses[i]
		earlyMood += moods[i]
	}
	for i := mid; i < len(stresses); i++ {
		lateStress += stresses[i]
		lateMood += moods[i]
	}
	n1, n2 := float64(mid), float64(len(stresses)-mid)
	earlyStress /= n1
	earlyMood /= n1
	lateStress /= n2
	lateMood /= n2

	var trends []string
	if d := lateStress - earlyStress; d > 0.7 {
		trends = append(trends, fmt.Sprintf("стресс растёт (%.1f→%.1f)", earlyStress, lateStress))
	} else if d < -0.7 {
		trends = append(trends, fmt.Sprintf("стресс снижается (%.1f→%.1f)", earlyStress, lateStress))
	}
	if d := lateMood - earlyMood; d < -0.7 {
		trends = append(trends, fmt.Sprintf("настроение падает (%.1f→%.1f)", earlyMood, lateMood))
	} else if d > 0.7 {
		trends = append(trends, fmt.Sprintf("настроение улучшается (%.1f→%.1f)", earlyMood, lateMood))
	}
	if len(trends) == 0 {
		return "", nil
	}
	return "Динамика 7 дней: " + strings.Join(trends, ", ") + ".", nil
}

// ── Stage 2: top event tags ───────────────────────────────────────────────────

func (b *ContextBuilder) fetchTopTags(ctx context.Context, userID int64, limit int) ([]string, error) {
	rows, err := b.db.Query(ctx,
		`SELECT unnest(tags) AS tag, COUNT(*) AS cnt
		 FROM mood_entries WHERE user_id = $1
		 GROUP BY tag ORDER BY cnt DESC LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		var cnt int
		if err := rows.Scan(&tag, &cnt); err == nil {
			tags = append(tags, tag)
		}
	}
	return tags, rows.Err()
}

// ── Stage 3: advice effectiveness ────────────────────────────────────────────

func (b *ContextBuilder) fetchEffectiveAdvice(ctx context.Context, userID int64) (string, error) {
	rows, err := b.db.Query(ctx,
		`SELECT advice_tag,
		        SUM(CASE WHEN followup_answer = 'yes' THEN 1 ELSE 0 END)::float / COUNT(*) AS rate
		 FROM advice_outcomes
		 WHERE user_id = $1 AND followup_answer IS NOT NULL
		 GROUP BY advice_tag
		 HAVING COUNT(*) >= 2
		 ORDER BY rate DESC LIMIT 3`,
		userID,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		var rate float64
		if err := rows.Scan(&tag, &rate); err == nil && rate >= 0.5 {
			tags = append(tags, tag)
		}
	}
	if len(tags) == 0 {
		return "", nil
	}
	return strings.Join(tags, ", "), rows.Err()
}

// ── Stage 3.5: weekly AI insights ────────────────────────────────────────────

func (b *ContextBuilder) fetchWeeklyInsights(ctx context.Context, userID int64) (string, error) {
	var raw []byte
	err := b.db.QueryRow(ctx,
		`SELECT data FROM insight_reports
		 WHERE user_id = $1 AND generated_at >= NOW() - INTERVAL '14 days'`,
		userID,
	).Scan(&raw)
	if err != nil || len(raw) == 0 {
		return "", nil
	}

	var report struct {
		Insights []struct {
			Body string `json:"body"`
			Tone string `json:"tone"`
		} `json:"insights"`
		PersonalInsights []struct {
			Body string `json:"body"`
			Tone string `json:"tone"`
		} `json:"personal_insights"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return "", nil
	}

	// Collect: warning-tone first, then personal, then neutral — up to 3 total
	var selected []string
	for _, ins := range report.Insights {
		if ins.Tone == "warning" && ins.Body != "" {
			selected = append(selected, ins.Body)
		}
	}
	for _, ins := range report.PersonalInsights {
		if ins.Body != "" {
			selected = append(selected, ins.Body)
		}
	}
	for _, ins := range report.Insights {
		if ins.Tone != "warning" && ins.Body != "" {
			selected = append(selected, ins.Body)
		}
	}
	if len(selected) > 3 {
		selected = selected[:3]
	}
	if len(selected) == 0 {
		return "", nil
	}
	return "Статистика недели: " + strings.Join(selected, " / ") + ".", nil
}

// ── Stage 3.7: recent LR sessions from previous days ─────────────────────────

func (b *ContextBuilder) fetchRecentSessions(ctx context.Context, userID int64) (string, error) {
	rows, err := b.db.Query(ctx,
		`SELECT s.event_tag, f.is_helpful
		 FROM live_response_sessions s
		 LEFT JOIN live_response_feedback f ON f.session_id = s.id
		 WHERE s.user_id = $1
		   AND DATE(s.created_at AT TIME ZONE 'UTC') < CURRENT_DATE
		 ORDER BY s.created_at DESC
		 LIMIT 3`,
		userID,
	)
	if err != nil {
		return "", nil
	}
	defer rows.Close()

	var parts []string
	for rows.Next() {
		var tag string
		var helpful *bool
		if err := rows.Scan(&tag, &helpful); err != nil {
			continue
		}
		switch {
		case helpful == nil:
			parts = append(parts, tag)
		case *helpful:
			parts = append(parts, tag+" (помогло)")
		default:
			parts = append(parts, tag+" (не помогло)")
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "Недавние разговоры (предыдущие дни): " + strings.Join(parts, ", ") + ".", nil
}

// ── Stage 4: mood–event correlations ─────────────────────────────────────────

func (b *ContextBuilder) fetchCorrelations(ctx context.Context, userID int64) (string, error) {
	rows, err := b.db.Query(ctx,
		`WITH overall AS (
		   SELECT AVG(score) AS avg FROM mood_entries WHERE user_id = $1
		 )
		 SELECT tag,
		        ROUND((AVG(m.score) - o.avg)::numeric, 1) AS delta
		 FROM mood_entries m
		 CROSS JOIN overall o,
		 unnest(m.tags) AS tag
		 WHERE m.user_id = $1
		 GROUP BY tag, o.avg
		 HAVING COUNT(*) >= 3
		 ORDER BY delta
		 LIMIT 2`,
		userID,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type corr struct {
		tag   string
		delta float64
	}
	var rows2 []corr
	for rows.Next() {
		var c corr
		if err := rows.Scan(&c.tag, &c.delta); err == nil {
			rows2 = append(rows2, c)
		}
	}
	_ = rows.Err()

	var corrParts []string
	for _, c := range rows2 {
		sign := "снижает"
		if c.delta > 0 {
			sign = "повышает"
		}
		corrParts = append(corrParts, fmt.Sprintf("«%s» %s настроение (Δ%.1f)", c.tag, sign, c.delta))
	}
	return strings.Join(corrParts, "; "), nil
}

// ── Base stats ────────────────────────────────────────────────────────────────

func (b *ContextBuilder) fetchBase(ctx context.Context, userID int64) (baseStats, error) {
	var s baseStats
	var firstAt time.Time
	err := b.db.QueryRow(ctx,
		`SELECT COUNT(*), MIN(created_at), COALESCE(AVG(score), 0)
		 FROM mood_entries WHERE user_id = $1`,
		userID,
	).Scan(&s.totalEntries, &firstAt, &s.avgScore)
	if err != nil || s.totalEntries == 0 {
		return s, err
	}
	s.daysActive = int(time.Since(firstAt).Hours()/24) + 1
	return s, nil
}
