package respond

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"

	chimw "github.com/go-chi/chi/v5/middleware"
)

var debugMode atomic.Bool

func SetDebugMode(enabled bool) { debugMode.Store(enabled) }
func IsDebug() bool             { return debugMode.Load() }

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func OK(w http.ResponseWriter, v any)      { JSON(w, http.StatusOK, v) }
func Created(w http.ResponseWriter, v any) { JSON(w, http.StatusCreated, v) }

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func BadRequest(w http.ResponseWriter, msg string)  { Error(w, http.StatusBadRequest, msg) }
func Unauthorized(w http.ResponseWriter)            { Error(w, http.StatusUnauthorized, "unauthorized") }
func NotFound(w http.ResponseWriter)                { Error(w, http.StatusNotFound, "not found") }

// Internal logs the error with a full stack trace and returns 500.
// In debug mode the error message and stack are included in the response body.
// In production only a generic message and request_id are returned so the caller
// can reference the request in server logs.
func Internal(w http.ResponseWriter, r *http.Request, err error) {
	stack := string(debug.Stack())
	reqID := chimw.GetReqID(r.Context())

	slog.Default().Error("internal server error",
		"request_id", reqID,
		"method", r.Method,
		"path", r.URL.Path,
		"err", err.Error(),
		"stack", stack,
	)

	if debugMode.Load() {
		JSON(w, http.StatusInternalServerError, map[string]any{
			"error":      "internal server error",
			"detail":     err.Error(),
			"stack":      stack,
			"request_id": reqID,
		})
		return
	}
	JSON(w, http.StatusInternalServerError, map[string]any{
		"error":      "internal server error",
		"request_id": reqID,
	})
}
