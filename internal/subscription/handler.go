package subscription

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

// Handler exposes subscription status to the mobile client.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/status", h.status)
	return r
}

// GET /api/v1/subscription/status
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	st, err := h.svc.GetStatus(r.Context(), userID)
	if err != nil {
		respond.Internal(w, err)
		return
	}
	respond.OK(w, st)
}
