package mood

import (
	"encoding/json"
	"errors"
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
	r.Post("/", h.create)
	r.Get("/", h.list)
	r.Get("/weekly", h.weekly)
	return r
}

type createRequest struct {
	Score       int      `json:"score"`
	StressLevel int      `json:"stress_level"`
	WorkHours   float64  `json:"work_hours"`
	Note        string   `json:"note"`
	Tags        []string `json:"tags"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	entry, err := h.svc.Create(r.Context(), CreateInput{
		UserID:      userID,
		Score:       req.Score,
		StressLevel: req.StressLevel,
		WorkHours:   req.WorkHours,
		Note:        req.Note,
		Tags:        req.Tags,
	})
	if err != nil {
		if errors.Is(err, ErrInvalidScore) {
			respond.BadRequest(w, err.Error())
			return
		}
		respond.Internal(w)
		return
	}
	respond.Created(w, entry)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	entries, err := h.svc.List(r.Context(), userID, page)
	if err != nil {
		respond.Internal(w)
		return
	}
	respond.OK(w, entries)
}

func (h *Handler) weekly(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	report, err := h.svc.WeeklyReport(r.Context(), userID)
	if err != nil {
		respond.Internal(w)
		return
	}
	respond.OK(w, report)
}
