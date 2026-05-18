package auth

import (
	"encoding/json"
	"errors"
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

func (h *Handler) Routes(jwtSvc *jwtutil.Service) http.Handler {
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Group(func(r chi.Router) {
		r.Use(jwtSvc.Middleware) // version check intentionally skipped: logout must work even with a stale token
		r.Post("/logout", h.logout)
	})
	return r
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	result, err := h.svc.Register(r.Context(), RegisterInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrEmailTaken):
			respond.Error(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrWeakPassword), errors.Is(err, ErrInvalidEmail):
			respond.BadRequest(w, err.Error())
		default:
			respond.Internal(w, err)
		}
		return
	}
	respond.Created(w, result)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	result, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCreds) {
			respond.Error(w, http.StatusUnauthorized, err.Error())
			return
		}
		respond.Internal(w, err)
		return
	}
	respond.OK(w, result)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	if err := h.svc.Logout(r.Context(), userID); err != nil {
		respond.Internal(w, err)
		return
	}
	respond.OK(w, map[string]string{"message": "logged out"})
}
