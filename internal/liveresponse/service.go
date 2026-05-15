package liveresponse

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// tempByType returns the inference temperature for each card variant.
// Advice cards use lower temperature for more concrete, repeatable answers.
func tempByType(rt ResponseType) float64 {
	if rt == ResponseAdvice {
		return 0.3
	}
	return 0.5
}

// Session holds the saved session data returned after generation.
type Session struct {
	ID               int64
	AIResponse       *AIResponse
	ResponseType     ResponseType
	FollowupHours    *int
	FollowupQuestion string
	IsLiteMode       bool
}

// Service orchestrates prompt assembly, DeepSeek call, and DB persistence.
type Service struct {
	db      *pgxpool.Pool
	client  *OpenRouterClient
	ctx_bld *ContextBuilder
}

func NewService(db *pgxpool.Pool, client *OpenRouterClient) *Service {
	return &Service{
		db:      db,
		client:  client,
		ctx_bld: NewContextBuilder(db),
	}
}

// Generate calls DeepSeek and saves the result to live_response_sessions.
func (s *Service) Generate(
	ctx context.Context,
	userID int64,
	trigger TriggerResult,
	selectedChip string,
) (*Session, error) {
	// 1. Lite Mode detection
	liteMode, err := IsLiteMode(ctx, s.db, userID)
	if err != nil {
		liteMode = false // non-fatal
	}

	// 2. Dynamic context from user history
	ctxText, err := s.ctx_bld.Build(ctx, userID)
	if err != nil {
		ctxText = "" // non-fatal: proceed without context
	}

	// 3. Select system prompt + lite mode modifier
	var systemPrompt string
	if trigger.Response == ResponseInstant {
		systemPrompt = SystemInstant
	} else {
		systemPrompt = SystemAdvice
	}
	if liteMode {
		systemPrompt += LiteModeInstruction
	}

	// 3. Assemble messages
	msgs := []Message{
		{Role: "system", Content: systemPrompt},
	}
	if ctxText != "" {
		msgs = append(msgs, Message{Role: "system", Content: ctxText})
	}
	msgs = append(msgs, Message{
		Role:    "user",
		Content: BuildUserPrompt(trigger.Tag, selectedChip),
	})

	// 4. Call DeepSeek via OpenRouter
	aiResp, err := s.client.Complete(ctx, msgs, tempByType(trigger.Response))
	if err != nil {
		return nil, err
	}

	// 5. Resolve follow-up hours: disabled in lite mode; else use AI suggestion or default
	var followupHoursPtr *int
	if !liteMode {
		followupHours := trigger.FollowupHours
		if aiResp.FollowupHours != nil && *aiResp.FollowupHours >= 1 && *aiResp.FollowupHours <= 12 {
			followupHours = *aiResp.FollowupHours
		}
		if followupHours > 0 {
			h := followupHours
			followupHoursPtr = &h
		}
	}

	// 6. Persist session
	sessionID, err := s.saveSession(ctx, userID, trigger, selectedChip, aiResp, followupHoursPtr)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:               sessionID,
		AIResponse:       aiResp,
		ResponseType:     trigger.Response,
		FollowupHours:    followupHoursPtr,
		FollowupQuestion: aiResp.FollowupQuestion,
		IsLiteMode:       liteMode,
	}, nil
}

// SaveFeedback records the helpful / not-helpful vote for a session.
func (s *Service) SaveFeedback(ctx context.Context, sessionID, userID int64, isHelpful bool) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO live_response_feedback (session_id, user_id, is_helpful)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (session_id) DO UPDATE SET is_helpful = EXCLUDED.is_helpful`,
		sessionID, userID, isHelpful,
	)
	return err
}

// FollowupAnswer is the input for AnswerFollowup.
type FollowupAnswer struct {
	Answer      string // "yes" | "no" | "skipped"
	ScheduledAt string // ISO-8601, when the notification fired on device
	RecheckChip string // optional chip chosen on the "no" screen
}

// FollowupResult is returned by AnswerFollowup.
type FollowupResult struct {
	// RecheckMessage is set only when Answer == "no" — a short AI follow-up.
	RecheckMessage string `json:"recheck_message,omitempty"`
}

// AnswerFollowup records the follow-up answer, writes advice_outcomes, and —
// if the user is still struggling — generates a short recheck advice.
func (s *Service) AnswerFollowup(
	ctx context.Context,
	sessionID, userID int64,
	input FollowupAnswer,
) (*FollowupResult, error) {
	// Load session to get event context
	var sess struct {
		EventTag    string
		AIMessage   string
		AdviceTags  []string
	}
	err := s.db.QueryRow(ctx,
		`SELECT event_tag, ai_message, advice_tags
		 FROM live_response_sessions WHERE id = $1 AND user_id = $2`,
		sessionID, userID,
	).Scan(&sess.EventTag, &sess.AIMessage, &sess.AdviceTags)
	if err != nil {
		return nil, err
	}

	status := map[string]string{
		"yes":     "answered_yes",
		"no":      "answered_no",
		"skipped": "skipped",
	}[input.Answer]
	if status == "" {
		status = "skipped"
	}

	// Insert followup row — use client-provided scheduled_at if valid, else NOW()
	if input.ScheduledAt != "" {
		_, err = s.db.Exec(ctx,
			`INSERT INTO followups (session_id, user_id, scheduled_at, status, answered_at)
			 VALUES ($1, $2, $3::timestamptz, $4, NOW())`,
			sessionID, userID, input.ScheduledAt, status,
		)
	} else {
		_, err = s.db.Exec(ctx,
			`INSERT INTO followups (session_id, user_id, scheduled_at, status, answered_at)
			 VALUES ($1, $2, NOW(), $3, NOW())`,
			sessionID, userID, status,
		)
	}
	if err != nil {
		return nil, err
	}

	// Write advice_outcomes — one row per advice_tag
	for _, tag := range sess.AdviceTags {
		_, _ = s.db.Exec(ctx,
			`INSERT INTO advice_outcomes (user_id, session_id, advice_tag, event_tag, followup_answer)
			 VALUES ($1, $2, $3, $4, $5)`,
			userID, sessionID, tag, sess.EventTag, input.Answer,
		)
	}

	result := &FollowupResult{}

	// User still struggling → generate short recheck response (plain text)
	if input.Answer == "no" {
		msgs := []Message{
			{Role: "system", Content: SystemRecheck},
			{Role: "user", Content: BuildRecheckPrompt(sess.EventTag, sess.AIMessage, input.RecheckChip)},
		}
		text, err := s.client.CompleteText(ctx, msgs, 0.5)
		if err == nil {
			result.RecheckMessage = text
		}
	}

	return result, nil
}

// ── Internal ──────────────────────────────────────────────────────────────────

func (s *Service) saveSession(
	ctx context.Context,
	userID int64,
	trigger TriggerResult,
	chip string,
	resp *AIResponse,
	followupHours *int,
) (int64, error) {
	var fhVal interface{} = nil
	if followupHours != nil {
		fhVal = *followupHours
	}

	var id int64
	err := s.db.QueryRow(ctx,
		`INSERT INTO live_response_sessions
		   (user_id, event_tag, event_weight, user_note, ai_message,
		    advice_tags, followup_hours, followup_question)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		userID,
		trigger.Tag,
		trigger.Weight,
		chip,
		resp.Message,
		resp.AdviceTags,
		fhVal,
		resp.FollowupQuestion,
	).Scan(&id)
	return id, err
}
