package weeklycard

import (
	"errors"
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

// @Summary      Get weekly card
// @Description  Returns the AI-generated weekly card for the current user. Returns 404 if not yet computed (computed every Sunday).
// @Tags         weekly-card
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string  "Card not yet generated"
// @Router       /weekly-card [get]
// GET /api/v1/weekly-card
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	card, err := h.svc.GetCard(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.NotFound(w)
			return
		}
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, card)
}
