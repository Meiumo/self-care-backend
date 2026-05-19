package liveresponse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/event-types", h.eventTypes)
	r.Post("/", h.generate)
	r.Post("/{id}/feedback", h.feedback)
	r.Post("/{id}/followup", h.followup)
	r.Post("/{id}/chat", h.chat)
	return r
}

// @Summary      Типы событий
// @Tags         live-response
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]any
// @Failure      500  {object}  map[string]string
// @Router       /live-response/event-types [get]
// GET /live-response/event-types
func (h *Handler) eventTypes(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.ListEventTypes(r.Context())
	if err != nil {
		respond.Internal(w, r, err)
		return
	}

	type eventDTO struct {
		Name          string   `json:"name"`
		Emoji         string   `json:"emoji"`
		Weight        int      `json:"weight"`
		FollowupHours int      `json:"followup_hours"`
		Chips         []string `json:"chips"`
		Opener        string   `json:"opener,omitempty"`
	}
	type grouped struct {
		Instant []eventDTO `json:"instant"`
		Advice  []eventDTO `json:"advice"`
		Nothing []eventDTO `json:"nothing"`
	}

	out := grouped{
		Instant: []eventDTO{},
		Advice:  []eventDTO{},
		Nothing: []eventDTO{},
	}
	for _, et := range all {
		chips := et.Chips
		if chips == nil {
			chips = []string{}
		}
		dto := eventDTO{
			Name:          et.Name,
			Emoji:         et.Emoji,
			Weight:        et.Weight,
			FollowupHours: et.FollowupHours,
			Chips:         chips,
			Opener:        et.Opener,
		}
		switch et.Response {
		case ResponseInstant:
			out.Instant = append(out.Instant, dto)
		case ResponseAdvice:
			out.Advice = append(out.Advice, dto)
		default:
			out.Nothing = append(out.Nothing, dto)
		}
	}
	respond.OK(w, out)
}

// POST /live-response
type generateRequest struct {
	EventTag     string `json:"event_tag"`
	SelectedChip string `json:"selected_chip"`
}

// @Summary      Генерация live-response
// @Tags         live-response
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      generateRequest  true  "Тег события и чип"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      402   {object}  map[string]string
// @Failure      422   {object}  map[string]string
// @Router       /live-response [post]
func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EventTag == "" {
		respond.BadRequest(w, "event_tag required")
		return
	}

	et, err := h.svc.FindEventType(r.Context(), req.EventTag)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "unknown event_tag")
		return
	}
	if et.Response == ResponseNothing {
		respond.Error(w, http.StatusUnprocessableEntity, "event type does not generate a live response")
		return
	}

	trigger := TriggerResult{
		Tag:           et.Name,
		Weight:        et.Weight,
		Response:      et.Response,
		FollowupHours: et.FollowupHours,
		Opener:        et.Opener,
	}

	// Detach from the request context: if the client closes the view mid-flight,
	// the DeepSeek call still completes and the result is saved to DB.
	// On the next open, tryResume returns it instantly instead of calling DeepSeek again.
	genCtx, genCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer genCancel()

	sess, err := h.svc.Generate(genCtx, userID, trigger, req.SelectedChip)
	if err != nil {
		if r.Context().Err() != nil {
			return // client disconnected; generation may still be running in background
		}
		respond.Internal(w, r, err)
		return
	}

	if r.Context().Err() != nil {
		return // client disconnected after generation completed, nothing to write
	}

	type chatMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type resp struct {
		SessionID        int64     `json:"session_id"`
		ResponseType     string    `json:"response_type"`
		Message          string    `json:"message"`
		AdviceTags       []string  `json:"advice_tags"`
		FollowupHours    *int      `json:"followup_hours"`
		FollowupQuestion string    `json:"followup_question"`
		Opener           string    `json:"opener,omitempty"`
		IsResumed        bool      `json:"is_resumed"`
		MessagesLeft     int       `json:"messages_left"`
		Messages         []chatMsg `json:"messages,omitempty"`
	}

	msgsLeft := maxChatUserMessages - sess.UserMsgCount
	if msgsLeft < 0 {
		msgsLeft = 0
	}

	var chatMsgs []chatMsg
	for _, m := range sess.ChatMessages {
		chatMsgs = append(chatMsgs, chatMsg{Role: m.Role, Content: m.Content})
	}

	respond.OK(w, resp{
		SessionID:        sess.ID,
		ResponseType:     sess.ResponseType.String(),
		Message:          sess.AIResponse.Message,
		AdviceTags:       sess.AIResponse.AdviceTags,
		FollowupHours:    sess.FollowupHours,
		FollowupQuestion: sess.FollowupQuestion,
		Opener:           sess.Opener,
		IsResumed:        sess.IsResumed,
		MessagesLeft:     msgsLeft,
		Messages:         chatMsgs,
	})
}

// POST /live-response/{id}/feedback
type feedbackRequest struct {
	IsHelpful bool `json:"is_helpful"`
}

// @Summary      Оставить отзыв на сессию
// @Tags         live-response
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int              true  "ID сессии"
// @Param        body  body      feedbackRequest  true  "Полезно или нет"
// @Success      200   {object}  map[string]string
// @Router       /live-response/{id}/feedback [post]
func (h *Handler) feedback(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respond.BadRequest(w, "invalid id")
		return
	}
	var req feedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.svc.SaveFeedback(r.Context(), sessionID, userID, req.IsHelpful); err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, map[string]string{"status": "ok"})
}

// POST /live-response/{id}/chat
type chatRequest struct {
	Message string `json:"message"`
}

// @Summary      Сообщение в чат
// @Tags         live-response
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int          true  "ID сессии"
// @Param        body  body      chatRequest  true  "Сообщение пользователя"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /live-response/{id}/chat [post]
func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respond.BadRequest(w, "invalid id")
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		respond.BadRequest(w, "message required")
		return
	}
	if utf8.RuneCountInString(req.Message) > 500 {
		respond.BadRequest(w, "message too long: max 500 characters")
		return
	}

	// Same detach pattern as generate: AI call + DB writes survive client disconnect.
	chatCtx, chatCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer chatCancel()

	reply, msgsLeft, err := h.svc.Chat(chatCtx, sessionID, userID, req.Message)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		if errors.Is(err, ErrMessageLimit) {
			respond.Error(w, http.StatusTooManyRequests, "message limit reached")
			return
		}
		if errors.Is(err, ErrSessionNotFound) {
			respond.Error(w, http.StatusNotFound, "session not found")
			return
		}
		respond.Internal(w, r, err)
		return
	}

	if r.Context().Err() != nil {
		return
	}

	type resp struct {
		Reply        string `json:"reply"`
		MessagesLeft int    `json:"messages_left"`
	}
	respond.OK(w, resp{Reply: reply, MessagesLeft: msgsLeft})
}

// POST /live-response/{id}/followup
type followupRequest struct {
	Answer      string `json:"answer"`       // "yes" | "no" | "skipped" | "declined"
	ScheduledAt string `json:"scheduled_at"` // ISO-8601
	RecheckChip string `json:"recheck_chip"` // optional chip chosen by user on "no" screen
}

// @Summary      Ответить на follow-up
// @Tags         live-response
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int              true  "ID сессии"
// @Param        body  body      followupRequest  true  "Ответ на follow-up"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Router       /live-response/{id}/followup [post]
func (h *Handler) followup(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	sessionID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respond.BadRequest(w, "invalid id")
		return
	}
	var req followupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if req.Answer != "yes" && req.Answer != "no" && req.Answer != "skipped" && req.Answer != "declined" {
		respond.BadRequest(w, "answer must be yes|no|skipped|declined")
		return
	}

	result, err := h.svc.AnswerFollowup(r.Context(), sessionID, userID, FollowupAnswer{
		Answer:      req.Answer,
		ScheduledAt: req.ScheduledAt,
		RecheckChip: req.RecheckChip,
	})
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, result)
}
