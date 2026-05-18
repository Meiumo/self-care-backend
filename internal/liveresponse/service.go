package liveresponse

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMessageLimit is returned when the user has sent maxChatUserMessages in a session.
var ErrMessageLimit = errors.New("message limit reached")

// ErrNothingEvent is returned when the caller tries to generate a response for a
// "nothing" event type (events that are only logged, not AI-processed).
var ErrNothingEvent = errors.New("event type does not generate a live response")

// Session holds the saved session data returned after generation.
type Session struct {
	ID               int64
	AIResponse       *AIResponse
	ResponseType     ResponseType
	FollowupHours    *int
	FollowupQuestion string
	Opener           string
	// IsResumed is true when an existing today's session was returned instead of generating a new one.
	IsResumed    bool
	ChatMessages []Message // populated only when IsResumed == true
	UserMsgCount int       // how many user messages have been sent (for messagesLeft)
}

// Service orchestrates prompt assembly, DeepSeek call, and DB persistence.
type Service struct {
	db      *pgxpool.Pool
	client  *OpenRouterClient
	ctx_bld *ContextBuilder
	evtRepo *EventTypeRepo
}

func NewService(db *pgxpool.Pool, client *OpenRouterClient) *Service {
	return &Service{
		db:      db,
		client:  client,
		ctx_bld: NewContextBuilder(db),
		evtRepo: NewEventTypeRepo(db),
	}
}

// FindEventType loads a single event type by name.
func (s *Service) FindEventType(ctx context.Context, name string) (*EventType, error) {
	return s.evtRepo.FindByName(ctx, name)
}

// ListEventTypes returns all event types ordered by sort_order.
func (s *Service) ListEventTypes(ctx context.Context) ([]*EventType, error) {
	return s.evtRepo.All(ctx)
}

// Generate calls DeepSeek and saves the result to live_response_sessions.
// If a session for the same event_tag already exists today, it is returned as-is
// (IsResumed == true, ChatMessages populated) without calling DeepSeek again.
// Returns ErrNothingEvent if the event type is ResponseNothing.
func (s *Service) Generate(
	ctx context.Context,
	userID int64,
	trigger TriggerResult,
	selectedChip string,
) (*Session, error) {
	if trigger.Response == ResponseNothing {
		return nil, ErrNothingEvent
	}

	// 0. Resume today's existing session if one exists for this event_tag
	if resumed, ok, err := s.tryResume(ctx, userID, trigger.Tag); err == nil && ok {
		resumed.ResponseType = trigger.Response
		resumed.Opener = trigger.Opener
		return resumed, nil
	}

	// 1. Dynamic context from user history
	ctxText, err := s.ctx_bld.Build(ctx, userID)
	if err != nil {
		ctxText = "" // non-fatal: proceed without context
	}

	// 2. Select system prompt
	var systemPrompt string
	if trigger.Response == ResponseInstant {
		systemPrompt = SystemInstant
	} else {
		systemPrompt = SystemAdvice
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

	// 5. Resolve follow-up hours
	var followupHoursPtr *int
	followupHours := trigger.FollowupHours
	if aiResp.FollowupHours != nil && *aiResp.FollowupHours >= 1 && *aiResp.FollowupHours <= 12 {
		followupHours = *aiResp.FollowupHours
	}
	if followupHours > 0 {
		h := followupHours
		followupHoursPtr = &h
	}

	// 6. Persist session.
	// Two concurrent requests for the same user+event_tag can both pass tryResume
	// (neither has a saved session yet) and race to call DeepSeek. The unique index
	// uq_live_session_user_tag_day ensures only one INSERT wins; the loser gets a
	// conflict error, re-runs tryResume, and returns the already-saved session.
	sessionID, err := s.saveSession(ctx, userID, trigger, selectedChip, aiResp, followupHoursPtr)
	if err != nil {
		if resumed, ok, rerr := s.tryResume(ctx, userID, trigger.Tag); rerr == nil && ok {
			resumed.ResponseType = trigger.Response
			resumed.Opener = trigger.Opener
			return resumed, nil
		}
		return nil, err
	}

	return &Session{
		ID:               sessionID,
		AIResponse:       aiResp,
		ResponseType:     trigger.Response,
		FollowupHours:    followupHoursPtr,
		FollowupQuestion: aiResp.FollowupQuestion,
		Opener:           trigger.Opener,
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
		"yes":      "answered_yes",
		"no":       "answered_no",
		"skipped":  "skipped",
		"declined": "declined",
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

// Chat handles one user message turn in a multi-turn live response session.
// Returns the AI reply, how many user messages remain, and any error.
func (s *Service) Chat(ctx context.Context, sessionID, userID int64, message string) (string, int, error) {
	// Load session: event_tag for system prompt, user_note for original trigger context,
	// ai_message for the first assistant turn.
	var eventTag, initialAI, userNote string
	if err := s.db.QueryRow(ctx,
		`SELECT event_tag, ai_message, user_note FROM live_response_sessions WHERE id = $1 AND user_id = $2`,
		sessionID, userID,
	).Scan(&eventTag, &initialAI, &userNote); err != nil {
		return "", 0, err
	}

	// ORDER BY created_at, id guarantees stable order even when timestamps collide.
	rows, err := s.db.Query(ctx,
		`SELECT role, content FROM live_response_messages WHERE session_id = $1 ORDER BY created_at, id`,
		sessionID,
	)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	var history []Message
	userCount := 0
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return "", 0, err
		}
		history = append(history, Message{Role: role, Content: content})
		if role == "user" {
			userCount++
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}

	if userCount >= maxChatUserMessages {
		return "", 0, ErrMessageLimit
	}

	// Fresh user context (today's state, trends, insights) — same as Generate.
	ctxText, _ := s.ctx_bld.Build(ctx, userID)

	// Reconstruct the full conversation: system → user context → original trigger
	// → first AI response → saved history → current message.
	msgs := []Message{
		{Role: "system", Content: SystemChat(eventTag)},
	}
	if ctxText != "" {
		msgs = append(msgs, Message{Role: "system", Content: ctxText})
	}
	msgs = append(msgs,
		Message{Role: "user", Content: BuildUserPrompt(eventTag, userNote)},
		Message{Role: "assistant", Content: initialAI},
	)
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: "user", Content: message})

	reply, err := s.client.CompleteText(ctx, msgs, 0.5)
	if err != nil {
		return "", 0, err
	}

	if _, err := s.db.Exec(ctx,
		`INSERT INTO live_response_messages (session_id, role, content) VALUES ($1, 'user', $2)`,
		sessionID, message,
	); err != nil {
		return "", 0, err
	}
	if _, err := s.db.Exec(ctx,
		`INSERT INTO live_response_messages (session_id, role, content) VALUES ($1, 'assistant', $2)`,
		sessionID, reply,
	); err != nil {
		return "", 0, err
	}

	return reply, maxChatUserMessages - userCount - 1, nil
}

// tryResume looks for a live_response_sessions row created today (UTC) for the
// given user and event_tag. If found, it loads the chat messages and returns a
// Session with IsResumed == true so the caller can skip DeepSeek entirely.
func (s *Service) tryResume(ctx context.Context, userID int64, eventTag string) (*Session, bool, error) {
	var (
		id        int64
		aiMessage string
		fhours    *int
		fquestion string
	)
	err := s.db.QueryRow(ctx,
		`SELECT id, ai_message, followup_hours, followup_question
		 FROM live_response_sessions
		 WHERE user_id = $1
		   AND event_tag = $2
		   AND created_at >= date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userID, eventTag,
	).Scan(&id, &aiMessage, &fhours, &fquestion)
	if err != nil {
		return nil, false, err // pgx.ErrNoRows is the common case — caller ignores it
	}

	rows, err := s.db.Query(ctx,
		`SELECT role, content FROM live_response_messages WHERE session_id = $1 ORDER BY created_at`,
		id,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var msgs []Message
	userCount := 0
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, false, err
		}
		msgs = append(msgs, Message{Role: role, Content: content})
		if role == "user" {
			userCount++
		}
	}

	sess := &Session{
		ID: id,
		AIResponse: &AIResponse{
			Message:          aiMessage,
			AdviceTags:       []string{},
			FollowupQuestion: fquestion,
		},
		FollowupHours:    fhours,
		FollowupQuestion: fquestion,
		IsResumed:        true,
		ChatMessages:     msgs,
		UserMsgCount:     userCount,
	}
	return sess, true, nil
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
