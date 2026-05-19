package user

import (
	"encoding/json"
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
	r.Get("/me", h.me)
	r.Put("/me", h.update)
	r.Post("/me/premium", h.setPremium)
	r.Get("/me/stats", h.stats)
	return r
}

// @Summary      Get current user profile
// @Description  Returns the profile of the authenticated user.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  user.DBUser
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /users/me [get]
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	profile, err := h.svc.Me(r.Context(), userID)
	if err != nil {
		respond.NotFound(w)
		return
	}
	respond.OK(w, profile)
}

type updateRequest struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// @Summary      Update profile
// @Description  Updates the display name and avatar URL of the current user.
// @Tags         users
// @Accept       json
// @Security     BearerAuth
// @Param        body  body  updateRequest  true  "Name and avatar URL"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /users/me [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.svc.UpdateProfile(r.Context(), userID, req.Name, req.AvatarURL); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary      User activity stats
// @Description  Returns streak, total entries logged, and other activity statistics for the current user.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  user.Stats
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /users/me/stats [get]
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	s, err := h.svc.GetStats(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, s)
}

type setPremiumRequest struct {
	Premium bool `json:"premium"`
}

// @Summary      Set own premium status
// @Description  Activates or deactivates premium for the current user (self-serve endpoint, typically for testing).
// @Tags         users
// @Accept       json
// @Security     BearerAuth
// @Param        body  body  setPremiumRequest  true  "Premium flag"
// @Success      204
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /users/me/premium [post]
func (h *Handler) setPremium(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req setPremiumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.svc.SetPremium(r.Context(), userID, req.Premium); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
