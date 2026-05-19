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

// @Summary      Get AI insights
// @Description  Returns the latest AI-generated insight report for the current user based on mood history.
// @Tags         analysis
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /analysis/insights [get]
func (h *Handler) insights(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	report, err := h.svc.GetInsights(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, report)
}

// @Summary      Insights history
// @Description  Returns all previously generated insight reports for the current user, newest first.
// @Tags         analysis
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /analysis/history [get]
func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	items, err := h.svc.History(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, items)
}

// @Summary      Regenerate insights
// @Description  Triggers immediate AI regeneration for the current user and returns the new report.
// @Tags         analysis
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /analysis/insights/refresh [post]
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
