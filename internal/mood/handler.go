package mood

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

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
	r.Get("/today", h.today)
	r.Get("/weekly", h.weekly)
	r.Get("/tags", h.tags)
	r.Patch("/{id}", h.update)
	return r
}

type createRequest struct {
	Score       int      `json:"score"`
	StressLevel int      `json:"stress_level"`
	WorkHours   float64  `json:"work_hours"`
	Note        string   `json:"note"`
	Tags        []string `json:"tags"`
}

// @Summary      Создать запись настроения
// @Tags         moods
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createRequest  true  "Данные настроения"
// @Success      201   {object}  mood.Entry
// @Failure      400   {object}  map[string]string
// @Router       /moods [post]
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
		respond.Internal(w, r, err)
		return
	}
	respond.Created(w, entry)
}

// @Summary      Список записей настроения
// @Tags         moods
// @Produce      json
// @Security     BearerAuth
// @Param        page  query     int  false  "Страница (по умолчанию 1)"
// @Success      200   {array}   mood.Entry
// @Failure      500   {object}  map[string]string
// @Router       /moods [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	entries, err := h.svc.List(r.Context(), userID, page)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, entries)
}

// @Summary      Недельный отчёт
// @Tags         moods
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  mood.WeeklyReport
// @Failure      500  {object}  map[string]string
// @Router       /moods/weekly [get]
func (h *Handler) weekly(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	report, err := h.svc.WeeklyReport(r.Context(), userID)
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, report)
}

// @Summary      Список тегов
// @Tags         moods
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   string
// @Failure      500  {object}  map[string]string
// @Router       /moods/tags [get]
func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.Tags(r.Context())
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, tags)
}

// @Summary      Запись настроения за сегодня
// @Tags         moods
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  mood.Entry
// @Failure      404  {object}  map[string]string
// @Router       /moods/today [get]
func (h *Handler) today(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	entry, err := h.svc.Today(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.NotFound(w)
			return
		}
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, entry)
}

type patchRequest struct {
	Score       int      `json:"score"`
	WorkHours   float64  `json:"work_hours"`
	StressLevel int      `json:"stress_level"`
	Tags        []string `json:"tags"`
}

// @Summary      Обновить запись настроения
// @Tags         moods
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      int           true  "ID записи"
// @Param        body  body      patchRequest  true  "Обновлённые данные"
// @Success      200   {object}  mood.Entry
// @Failure      400   {object}  map[string]string
// @Router       /moods/{id} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req patchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.BadRequest(w, "invalid json")
		return
	}
	entry, err := h.svc.Update(r.Context(), UpdateInput{
		ID: id, UserID: userID,
		Score: req.Score, WorkHours: req.WorkHours, StressLevel: req.StressLevel, Tags: req.Tags,
	})
	if err != nil {
		respond.Internal(w, r, err)
		return
	}
	respond.OK(w, entry)
}
