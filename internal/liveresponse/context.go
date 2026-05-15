package liveresponse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ContextBuilder assembles the dynamic context prompt based on how long
// the user has been active. Longer history → richer context.
type ContextBuilder struct {
	db *pgxpool.Pool
}

func NewContextBuilder(db *pgxpool.Pool) *ContextBuilder {
	return &ContextBuilder{db: db}
}

// Build returns the context prompt string for the given user.
// Returns empty string if there is no data yet — the system prompt is still valid.
func (b *ContextBuilder) Build(ctx context.Context, userID int64) (string, error) {
	base, err := b.fetchBase(ctx, userID)
	if err != nil || base.totalEntries == 0 {
		return "", err
	}

	days := base.daysActive
	var parts []string
	parts = append(parts, fmt.Sprintf(
		"Контекст пользователя: %d отметок настроения за %d дней. Средний балл настроения: %.1f/10.",
		base.totalEntries, days, base.avgScore,
	))

	// Stage 2 (4–7 days): add most frequent events
	if days >= 4 {
		tags, err := b.fetchTopTags(ctx, userID, 3)
		if err == nil && len(tags) > 0 {
			parts = append(parts, fmt.Sprintf(
				"Самые частые события: %s.", strings.Join(tags, ", "),
			))
		}
	}

	// Stage 3 (8–30 days): add advice effectiveness
	if days >= 8 {
		effective, err := b.fetchEffectiveAdvice(ctx, userID)
		if err == nil && effective != "" {
			parts = append(parts, "Советы, которые реально помогли ранее: "+effective+".")
		}
	}

	// Stage 4 (30+ days): add mood–event correlations
	if days >= 30 {
		corr, err := b.fetchCorrelations(ctx, userID)
		if err == nil && corr != "" {
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

// ── Queries ───────────────────────────────────────────────────────────────────

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

func (b *ContextBuilder) fetchCorrelations(ctx context.Context, userID int64) (string, error) {
	// Worst and best event by average mood score on days the event was present vs overall.
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

	type corr struct{ tag string; delta float64 }
	var rows2 []corr
	for rows.Next() {
		var c corr
		if err := rows.Scan(&c.tag, &c.delta); err == nil {
			rows2 = append(rows2, c)
		}
	}
	_ = rows.Err()

	var parts []string
	for _, c := range rows2 {
		sign := "снижает"
		if c.delta > 0 {
			sign = "повышает"
		}
		parts = append(parts, fmt.Sprintf("«%s» %s настроение (Δ%.1f)", c.tag, sign, c.delta))
	}
	return strings.Join(parts, "; "), nil
}
