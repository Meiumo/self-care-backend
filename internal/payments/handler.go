package payments

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

// Handler exposes payment endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// CreateRoutes returns routes that require JWT auth (mounted under /payments/create).
func (h *Handler) CreateRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.create)
	return r
}

// WebhookRoutes returns the unauthenticated webhook route.
func (h *Handler) WebhookRoutes() http.Handler {
	r := chi.NewRouter()
	r.Post("/", h.webhook)
	return r
}

// @Summary      Create payment link
// @Description  Generates a Prodamus payment URL for the premium subscription. Redirect the user to the returned URL.
// @Tags         payments
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  map[string]string  "payment_url"
// @Failure      401  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /payments/create [post]
// POST /api/v1/payments/create
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID := jwtutil.UserID(r.Context())
	payURL := h.svc.CreatePaymentURL(userID)
	respond.OK(w, map[string]string{"payment_url": payURL})
}

// @Summary      Payment webhook (Prodamus)
// @Description  Called by Prodamus after a successful payment to activate premium for the user. Validates HMAC signature.
// @Tags         payments
// @Accept       application/octet-stream
// @Param        X-Signature  header  string  true  "HMAC request signature"
// @Success      200
// @Failure      400  {object}  map[string]string  "Invalid signature or body"
// @Router       /payments/webhook [post]
// POST /api/v1/payments/webhook
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		respond.BadRequest(w, "cannot read body")
		return
	}
	sig := r.Header.Get("X-Signature")
	if err := h.svc.HandleWebhook(r.Context(), body, sig); err != nil {
		respond.Internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}
