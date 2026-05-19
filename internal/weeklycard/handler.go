package weeklycard

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

// Handler exposes the weekly card endpoint.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/", h.get)
	return r
}

// GET /api/v1/weekly-card
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	card, err := h.svc.GetCard(r.Context(), userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			respond.NotFound(w)
			return
		}
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, card)
}
