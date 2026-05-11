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
	return r
}

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

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.svc.UpdateProfile(r.Context(), userID, req.Name, req.AvatarURL); err != nil {
		respond.Internal(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setPremiumRequest struct {
	Premium bool `json:"premium"`
}

func (h *Handler) setPremium(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req setPremiumRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if err := h.svc.SetPremium(r.Context(), userID, req.Premium); err != nil {
		respond.Internal(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
