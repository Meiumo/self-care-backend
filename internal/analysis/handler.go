package analysis

import (
	"net/http"

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
	r.Get("/insights", h.insights)
	r.Post("/insights/refresh", h.refresh)
	r.Get("/history", h.history)
	return r
}

func (h *Handler) insights(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	report, err := h.svc.GetInsights(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, report)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	items, err := h.svc.History(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, items)
}

// refresh triggers insight regeneration for the current user only (for testing).
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	if err := h.svc.RegenerateForUser(r.Context(), userID); err != nil {
		respond.Internal(w, r, err)
		return
	}
	report, err := h.svc.GetInsights(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, report)
}
