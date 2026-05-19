package notification

import (
	"net/http"
	"strconv"

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
	r.Get("/", h.list)
	r.Get("/unread-count", h.unreadCount)
	r.Post("/read-all", h.readAll)
	r.Patch("/{id}/read", h.markRead)
	return r
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	notifs, err := h.svc.List(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, notifs)
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	count, err := h.svc.UnreadCount(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, map[string]int{"count": count})
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	if err := h.svc.MarkAllRead(r.Context(), userID); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.svc.MarkRead(r.Context(), id, userID); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
