package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/romangolovachev/selfcare/internal/analysis"
	"github.com/romangolovachev/selfcare/internal/liveresponse"
	"github.com/romangolovachev/selfcare/internal/notification"
	"github.com/romangolovachev/selfcare/internal/weeklycard"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

type Handler struct {
	repo     *Repository
	notif    *notification.Service
	analysis *analysis.Service
	wc       *weeklycard.Service
	lr       *liveresponse.Service
}

func NewHandler(
	repo *Repository,
	notif *notification.Service,
	analysis *analysis.Service,
	wc *weeklycard.Service,
	lr *liveresponse.Service,
) *Handler {
	return &Handler{repo: repo, notif: notif, analysis: analysis, wc: wc, lr: lr}
}

// RequireAdmin rejects requests from non-admin users with 403.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := jwtutil.UserID(r.Context())
		ok, err := h.repo.IsAdmin(r.Context(), userID)
		if err != nil || !ok {
			respond.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(h.RequireAdmin)

	r.Get("/stats", h.stats)
	r.Get("/users", h.listUsers)
	r.Post("/users/{id}/premium", h.setPremium)
	r.Patch("/users/{id}/admin", h.setAdmin)

	r.Post("/notifications/test", h.testNotification)
	r.Post("/notifications/reminders", h.triggerReminders)

	r.Post("/analysis/regen", h.regenAnalysis)
	r.Post("/weekly-card/regen", h.regenWeeklyCard)
	r.Post("/weekly-card/recompute", h.recomputeWeeklyCard)

	r.Post("/live-response/trigger", h.triggerLR)
	r.Post("/live-response/test-followup", h.testFollowup)

	r.Get("/errors", h.listErrors)

	return r
}

// @Summary      Platform statistics
// @Description  Returns aggregate platform stats: total users, active today, premium count, mood entries, and more.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  admin.StatsRow
// @Failure      403  {object}  map[string]string
// @Router       /admin/stats [get]
// GET /admin/stats
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	s, err := h.repo.Stats(r.Context())
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, s)
}

// @Summary      List all users
// @Description  Returns a full list of users with their roles, subscription status, and registration date.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   admin.UserRow
// @Failure      403  {object}  map[string]string
// @Router       /admin/users [get]
// GET /admin/users
func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.repo.ListUsers(r.Context())
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	if users == nil {
		users = []UserRow{}
	}
	respond.OK(w, users)
}

// @Summary      Set user premium
// @Description  Grants or revokes premium access for the specified user.
// @Tags         admin
// @Accept       json
// @Security     BearerAuth
// @Param        id    path  int                        true  "User ID"
// @Param        body  body  object{premium=bool}       true  "Premium flag"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/users/{id}/premium [post]
func (h *Handler) setPremium(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respond.BadRequest(w, "invalid id")
		return
	}
	var req struct {
		Premium bool `json:"premium"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.repo.SetPremium(r.Context(), userID, req.Premium); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Set user admin role
// @Description  Grants or revokes admin role for the specified user.
// @Tags         admin
// @Accept       json
// @Security     BearerAuth
// @Param        id    path  int                   true  "User ID"
// @Param        body  body  object{admin=bool}    true  "Admin flag"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/users/{id}/admin [patch]
func (h *Handler) setAdmin(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		respond.BadRequest(w, "invalid id")
		return
	}
	var req struct {
		Admin bool `json:"admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.repo.SetAdmin(r.Context(), userID, req.Admin); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Send test notification
// @Description  Sends a push notification to a specific user for testing. Default type is mood_reminder.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      object{user_id=int,type=string,title=string,body=string}  true  "Notification payload"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/notifications/test [post]
func (h *Handler) testNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID int64  `json:"user_id"`
		Type   string `json:"type"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 || req.Title == "" {
		respond.BadRequest(w, "user_id and title required")
		return
	}
	if req.Type == "" {
		req.Type = notification.TypeMoodReminder
	}
	if err := h.notif.Push(r.Context(), req.UserID, req.Type, req.Title, req.Body); err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, map[string]string{"status": "sent"})
}

// @Summary      Trigger daily reminders
// @Description  Runs the daily reminder job immediately in the background.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/notifications/reminders [post]
func (h *Handler) triggerReminders(w http.ResponseWriter, r *http.Request) {
	go h.notif.SendDailyReminders(context.Background())
	respond.OK(w, map[string]string{"status": "triggered"})
}

// @Summary      Regenerate all insights
// @Description  Triggers AI insight regeneration for all users in the background.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/analysis/regen [post]
func (h *Handler) regenAnalysis(w http.ResponseWriter, r *http.Request) {
	go h.analysis.RegenerateAll(context.Background())
	respond.OK(w, map[string]string{"status": "triggered"})
}

// @Summary      Regenerate all weekly cards
// @Description  Triggers weekly card recomputation for all users in the background.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/weekly-card/regen [post]
func (h *Handler) regenWeeklyCard(w http.ResponseWriter, r *http.Request) {
	go h.wc.ComputeAll(context.Background())
	respond.OK(w, map[string]string{"status": "triggered"})
}

// @Summary      Recompute my weekly card
// @Description  Recomputes the weekly card for the calling admin user immediately.
// @Tags         admin
// @Security     BearerAuth
// @Success      204
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/weekly-card/recompute [post]
func (h *Handler) recomputeWeeklyCard(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	if err := h.wc.ComputeForUser(r.Context(), userID); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Trigger live response for user
// @Description  Manually generates a live-response session for a specific user and event type. Use force=true to reset an existing session for today.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      object{user_id=int,event_tag=string,selected_chip=string,force=bool}  true  "Trigger payload"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string  "Unknown event_tag"
// @Router       /admin/live-response/trigger [post]
func (h *Handler) triggerLR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID       int64  `json:"user_id"`
		EventTag     string `json:"event_tag"`
		SelectedChip string `json:"selected_chip"`
		Force        bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == 0 || req.EventTag == "" {
		respond.BadRequest(w, "user_id and event_tag required")
		return
	}

	et, err := h.lr.FindEventType(r.Context(), req.EventTag)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "unknown event_tag")
		return
	}
	if et.Response == liveresponse.ResponseNothing {
		respond.Error(w, http.StatusUnprocessableEntity, "event type does not generate a live response")
		return
	}

	if req.Force {
		_ = h.repo.DeleteLRSession(r.Context(), req.UserID, req.EventTag)
	}

	trigger := liveresponse.TriggerResult{
		Tag:           et.Name,
		Weight:        et.Weight,
		Response:      et.Response,
		FollowupHours: et.FollowupHours,
		Opener:        et.Opener,
	}

	sess, err := h.lr.Generate(r.Context(), req.UserID, trigger, req.SelectedChip)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}

	respond.OK(w, map[string]any{
		"session_id":    sess.ID,
		"response_type": sess.ResponseType.String(),
		"message":       sess.AIResponse.Message,
		"advice_tags":   sess.AIResponse.AdviceTags,
		"is_resumed":    sess.IsResumed,
	})
}

// @Summary      Test follow-up notification
// @Description  Creates a fake session (no AI call) and returns a followup_at timestamp for testing push notification scheduling. Default delay is 30 seconds.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      object{event_tag=string,delay_seconds=int}  true  "Test payload"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /admin/live-response/test-followup [post]
func (h *Handler) testFollowup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventTag     string `json:"event_tag"`
		DelaySeconds int    `json:"delay_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.EventTag == "" {
		respond.BadRequest(w, "event_tag required")
		return
	}
	if req.DelaySeconds <= 0 {
		req.DelaySeconds = 30
	}

	userID := jwtutil.UserID(r.Context())
	sessionID, err := h.repo.CreateTestLRSession(r.Context(), userID, req.EventTag)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}

	followupAt := time.Now().Add(time.Duration(req.DelaySeconds) * time.Second).UTC()

	respond.OK(w, map[string]any{
		"session_id":        sessionID,
		"followup_at":       followupAt.Format(time.RFC3339),
		"followup_question": "Как ты сейчас?",
		"message":           "[ТЕСТ] Тестовая сессия для проверки follow-up уведомления.",
	})
}

// @Summary      Error log
// @Description  Returns recent application errors logged to the database, including request ID, path, status, and stack trace.
// @Tags         admin
// @Produce      json
// @Security     BearerAuth
// @Param        limit  query     int  false  "Max entries to return (default 100, max 1000)"
// @Success      200    {array}   admin.ErrorEntry
// @Failure      403    {object}  map[string]string
// @Router       /admin/errors [get]
// GET /admin/errors?limit=100
func (h *Handler) listErrors(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}
	entries, err := h.repo.ListErrors(r.Context(), limit)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	if entries == nil {
		entries = []ErrorEntry{}
	}
	respond.OK(w, entries)
}
