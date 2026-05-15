package liveresponse

// EventKind distinguishes negative (stress-generating) from positive events.
type EventKind int

const (
	KindNegative EventKind = iota
	KindPositive
)

// ResponseType defines which Live Response card variant to show.
type ResponseType int

const (
	// ResponseInstant — card with optional chip-input, DeepSeek replies
	// immediately, follow-up in 1–2 h.
	ResponseInstant ResponseType = iota
	// ResponseAdvice — advice-only card (no text input), follow-up in 3–12 h.
	ResponseAdvice
)

// eventPriority holds all Live Response metadata for a single event tag.
type eventPriority struct {
	Weight       int
	Kind         EventKind
	Response     ResponseType
	FollowupHours int // hours until the follow-up notification
}

// eventWeights is the single source of truth for Live Response prioritisation
// and card behaviour.
//
// Weight tiers (negative):
//
//	4 — critical  → ResponseInstant  (Конфликт, Дедлайн, Критика, Тревога)
//	3 — high      → ResponseAdvice   (Переработка, Недосып, Много задач)
//	2 — medium    → ResponseAdvice   (Устал, Встреча с руководителем, Переговоры)
//	1 — low       → ResponseAdvice   (Обратная связь, Другое, Скучно)
//
// Positive tiers → ResponseInstant:
//
//	3 — strong  (Похвала, Командный успех)
//	2 — mild    (Решил сложную задачу, Хорошая новость)
var eventWeights = map[string]eventPriority{
	// ── Critical → Instant, 1 h ─────────────────────────────
	"Конфликт": {Weight: 4, Kind: KindNegative, Response: ResponseInstant, FollowupHours: 1},
	"Критика":  {Weight: 4, Kind: KindNegative, Response: ResponseInstant, FollowupHours: 1},
	"Тревога":  {Weight: 4, Kind: KindNegative, Response: ResponseInstant, FollowupHours: 1},
	"Дедлайн":  {Weight: 4, Kind: KindNegative, Response: ResponseInstant, FollowupHours: 2},

	// ── High → Advice, 4–8 h ────────────────────────────────
	"Переработка": {Weight: 3, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 8},
	"Недосып":     {Weight: 3, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 6},
	"Много задач": {Weight: 3, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 4},

	// ── Medium → Advice, 3–6 h ──────────────────────────────
	"Устал":                   {Weight: 2, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 4},
	"Встреча с руководителем": {Weight: 2, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 3},
	"Переговоры":              {Weight: 2, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 3},

	// ── Low → Advice, 6–12 h ────────────────────────────────
	"Обратная связь": {Weight: 1, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 6},
	"Другое":         {Weight: 1, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 6},
	"Скучно":         {Weight: 1, Kind: KindNegative, Response: ResponseAdvice, FollowupHours: 12},

	// ── Positive → Instant ──────────────────────────────────
	"Похвала":              {Weight: 3, Kind: KindPositive, Response: ResponseInstant, FollowupHours: 2},
	"Командный успех":      {Weight: 3, Kind: KindPositive, Response: ResponseInstant, FollowupHours: 2},
	"Решил сложную задачу": {Weight: 2, Kind: KindPositive, Response: ResponseInstant, FollowupHours: 2},
	"Хорошая новость":      {Weight: 2, Kind: KindPositive, Response: ResponseInstant, FollowupHours: 2},
}

// TriggerResult is the selected event that will drive the Live Response.
type TriggerResult struct {
	Tag           string
	Weight        int
	Kind          EventKind
	Response      ResponseType
	FollowupHours int
}

// PickTrigger selects the single highest-priority event from the session tags.
//
// Rules:
//  1. Prefer the negative event with the highest weight.
//  2. If there are no negative events, fall back to the positive event with
//     the highest weight.
//  3. Tags absent from eventWeights are treated as weight-1 negative events
//     (safe default, still generates a response).
//  4. Tie-breaking: the tag that appears first in the input slice wins,
//     making the result deterministic for the same input order.
func PickTrigger(tags []string) (TriggerResult, bool) {
	if len(tags) == 0 {
		return TriggerResult{}, false
	}

	var bestNeg, bestPos *TriggerResult

	for _, tag := range tags {
		p, known := eventWeights[tag]
		if !known {
			p = eventPriority{Weight: 1, Kind: KindNegative}
		}

		candidate := TriggerResult{Tag: tag, Weight: p.Weight, Kind: p.Kind, Response: p.Response, FollowupHours: p.FollowupHours}

		switch p.Kind {
		case KindNegative:
			if bestNeg == nil || candidate.Weight > bestNeg.Weight {
				c := candidate
				bestNeg = &c
			}
		case KindPositive:
			if bestPos == nil || candidate.Weight > bestPos.Weight {
				c := candidate
				bestPos = &c
			}
		}
	}

	if bestNeg != nil {
		return *bestNeg, true
	}
	if bestPos != nil {
		return *bestPos, true
	}
	return TriggerResult{}, false
}

// EventWeight returns the raw weight of a single tag (0 if unknown).
func EventWeight(tag string) int {
	if p, ok := eventWeights[tag]; ok {
		return p.Weight
	}
	return 0
}

// ResponseConfig returns the card type and default follow-up hours for a tag.
// Unknown tags default to ResponseAdvice / 6 h.
func ResponseConfig(tag string) (ResponseType, int) {
	if p, ok := eventWeights[tag]; ok {
		return p.Response, p.FollowupHours
	}
	return ResponseAdvice, 6
}
