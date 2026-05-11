package analysis

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
	r.Post("/text", h.analyzeText)
	return r
}

type analyzeRequest struct {
	Text string `json:"text"`
}

func (h *Handler) analyzeText(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	if len(req.Text) < 10 {
		respond.BadRequest(w, "text too short")
		return
	}
	if len(req.Text) > 10000 {
		respond.BadRequest(w, "text too long (max 10000 chars)")
		return
	}

	result, err := h.svc.AnalyzeText(r.Context(), req.Text)
	if err != nil {
		respond.Internal(w)
		return
	}
	respond.OK(w, result)
}
