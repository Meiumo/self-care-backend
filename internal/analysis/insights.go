package analysis

import (
	"fmt"
	"strings"
	"time"
)

type entryData struct {
	Score       int
	StressLevel int
	WorkHours   float64
	Tags        []string
	CreatedAt   time.Time
}

type Insight struct {
	Icon  string `json:"icon"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Tone  string `json:"tone"` // "positive" | "warning" | "neutral"
}

type Advice struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

type InsightReport struct {
	Insights         []Insight `json:"insights"`
	Advice           []Advice  `json:"advice"`
	// PersonalInsights are appended by the personalinsights daily cron job.
	PersonalInsights []Insight `json:"personal_insights,omitempty"`
}

// InsightTemplate holds one rule's display strings loaded from insight_templates.
type InsightTemplate struct {
	Icon  string
	Title string // placeholders: {tag} {delta} {count} {avg}
	Body  string
	Tone  string
}

// Templates bundles DB-loaded template maps passed into computeInsights.
type Templates struct {
	Insights map[string]InsightTemplate // keyed by rule name
	Advice   map[string]string          // tag → advice text
}

// apply replaces {tag}, {delta}, {count}, {avg} in a template string.
func apply(tpl string, vars map[string]string) string {
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{"+k+"}", v)
	}
	return tpl
}

func insight(tpl InsightTemplate, vars map[string]string) Insight {
	return Insight{
		Icon:  tpl.Icon,
		Title: apply(tpl.Title, vars),
		Body:  apply(tpl.Body, vars),
		Tone:  tpl.Tone,
	}
}

func avgFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range vals {
		s += v
	}
	return s / float64(len(vals))
}

func scoreToLevel(score int) float64 {
	return float64(score) / 2.0 // 1.0–5.0 display range
}

func computeInsights(entries []entryData, tpl Templates) InsightReport {
	if len(entries) == 0 {
		nd := tpl.Insights["no_data"]
		return InsightReport{
			Insights: []Insight{{Icon: nd.Icon, Title: nd.Title, Body: nd.Body, Tone: nd.Tone}},
			Advice:   []Advice{},
		}
	}

	var insights []Insight

	// ── Общее среднее настроение ──────────────────────────────────────────────
	var allLevels []float64
	for _, e := range entries {
		allLevels = append(allLevels, scoreToLevel(e.Score))
	}
	avgAll := avgFloat(allLevels)

	// ── 1. Тег с наибольшим негативным влиянием ───────────────────────────────
	tagLevels := map[string][]float64{}
	for _, e := range entries {
		lvl := scoreToLevel(e.Score)
		for _, t := range e.Tags {
			tagLevels[t] = append(tagLevels[t], lvl)
		}
	}

	worstTag, worstDelta := "", 0.0
	for tag, lvls := range tagLevels {
		if len(lvls) < 2 {
			continue
		}
		d := avgFloat(lvls) - avgAll
		if d < worstDelta {
			worstDelta = d
			worstTag = tag
		}
	}
	if worstTag != "" && worstDelta < -0.3 {
		if t, ok := tpl.Insights["worst_tag"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"tag":   worstTag,
				"delta": fmt.Sprintf("%.1f", -worstDelta),
			}))
		}
	}

	// ── 2. Тег с наибольшим позитивным влиянием ───────────────────────────────
	bestTag, bestDelta := "", 0.0
	for tag, lvls := range tagLevels {
		if len(lvls) < 2 {
			continue
		}
		d := avgFloat(lvls) - avgAll
		if d > bestDelta {
			bestDelta = d
			bestTag = tag
		}
	}
	if bestTag != "" && bestDelta > 0.3 {
		if t, ok := tpl.Insights["best_tag"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"tag":   bestTag,
				"delta": fmt.Sprintf("%.1f", bestDelta),
			}))
		}
	}

	// ── 3. Переработки ────────────────────────────────────────────────────────
	overworkDays := 0
	var overworkH []float64
	for _, e := range entries {
		if e.WorkHours > 8 {
			overworkDays++
			overworkH = append(overworkH, e.WorkHours)
		}
	}
	if overworkDays >= 2 {
		if t, ok := tpl.Insights["overwork_many"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"count": fmt.Sprintf("%d", overworkDays),
				"avg":   fmt.Sprintf("%.1f", avgFloat(overworkH)),
			}))
		}
	} else if overworkDays == 1 {
		if t, ok := tpl.Insights["overwork_one"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"avg": fmt.Sprintf("%.1f", overworkH[0]),
			}))
		}
	}

	// ── 4. Тренд стресса ──────────────────────────────────────────────────────
	if len(entries) >= 4 {
		half := len(entries) / 2
		var recentS, olderS []float64
		for _, e := range entries[:half] {
			recentS = append(recentS, float64(e.StressLevel))
		}
		for _, e := range entries[half:] {
			olderS = append(olderS, float64(e.StressLevel))
		}
		delta := avgFloat(recentS) - avgFloat(olderS)
		if delta > 1.0 {
			if t, ok := tpl.Insights["stress_up"]; ok {
				insights = append(insights, insight(t, map[string]string{
					"delta": fmt.Sprintf("%.1f", delta),
				}))
			}
		} else if delta < -1.0 {
			if t, ok := tpl.Insights["stress_down"]; ok {
				insights = append(insights, insight(t, map[string]string{
					"delta": fmt.Sprintf("%.1f", -delta),
				}))
			}
		}
	}

	// ── 5. Топ-теги при малом объёме данных ──────────────────────────────────
	if len(entries) < 5 && len(tagLevels) > 0 {
		// Pick up to 2 most frequent tags
		type tagCount struct{ tag string; n int }
		var ranked []tagCount
		for tag, lvls := range tagLevels {
			ranked = append(ranked, tagCount{tag, len(lvls)})
		}
		// simple selection sort for top-2
		for i := 0; i < len(ranked)-1; i++ {
			for j := i + 1; j < len(ranked); j++ {
				if ranked[j].n > ranked[i].n {
					ranked[i], ranked[j] = ranked[j], ranked[i]
				}
			}
		}
		top := ranked
		if len(top) > 2 {
			top = top[:2]
		}
		tagStr := "«" + top[0].tag + "»"
		if len(top) > 1 {
			tagStr += " и «" + top[1].tag + "»"
		}
		if t, ok := tpl.Insights["top_tags"]; ok {
			insights = append(insights, insight(t, map[string]string{"tags": tagStr}))
		}
	}

	// ── 6. Общий уровень настроения / регулярность ────────────────────────────
	if len(entries) < 5 {
		if t, ok := tpl.Insights["regularity"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"count": fmt.Sprintf("%d", len(entries)),
			}))
		}
	} else if avgAll >= 4.0 {
		if t, ok := tpl.Insights["mood_good"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"avg": fmt.Sprintf("%.1f", avgAll),
			}))
		}
	} else if avgAll < 3.0 {
		if t, ok := tpl.Insights["mood_bad"]; ok {
			insights = append(insights, insight(t, map[string]string{
				"avg": fmt.Sprintf("%.1f", avgAll),
			}))
		}
	}

	if len(insights) > 5 {
		insights = insights[:5]
	}

	// ── Советы по тегам из последних записей ─────────────────────────────────
	seen := map[string]bool{}
	var advice []Advice
	for _, e := range entries {
		for _, tag := range e.Tags {
			if seen[tag] {
				continue
			}
			if text, ok := tpl.Advice[tag]; ok {
				advice = append(advice, Advice{Tag: tag, Text: text})
				seen[tag] = true
			}
			if len(advice) >= 3 {
				break
			}
		}
		if len(advice) >= 3 {
			break
		}
	}

	return InsightReport{Insights: insights, Advice: advice}
}
