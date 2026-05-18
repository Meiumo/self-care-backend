package liveresponse

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

// GET /live-response/event-types
func (h *Handler) eventTypes(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.ListEventTypes(r.Context())
	if err != nil {
		respond.Internal(w, err)
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

	sess, err := h.svc.Generate(r.Context(), userID, trigger, req.SelectedChip)
	if err != nil {
		respond.Internal(w, err)
		return
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
		respond.Internal(w, err)
		return
	}
	respond.OK(w, map[string]string{"status": "ok"})
}

// POST /live-response/{id}/chat
type chatRequest struct {
	Message string `json:"message"`
}

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

	reply, msgsLeft, err := h.svc.Chat(r.Context(), sessionID, userID, req.Message)
	if err != nil {
		if errors.Is(err, ErrMessageLimit) {
			respond.Error(w, http.StatusTooManyRequests, "message limit reached")
			return
		}
		respond.Internal(w, err)
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
		respond.Internal(w, err)
		return
	}
	respond.OK(w, result)
}
