package respond

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
)

// debugMode is set once at startup via SetDebugMode.
// Uses atomic to avoid any data race if somehow called concurrently.
var debugMode atomic.Bool

// SetDebugMode enables full error details in Internal responses.
// Call once during application startup based on APP_ENV.
func SetDebugMode(enabled bool) {
	debugMode.Store(enabled)
}

// IsDebug reports whether debug mode is active.
func IsDebug() bool { return debugMode.Load() }

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

// Internal logs err and returns 500 to the client.
// In debug mode the full error string is included in the response body.
func Internal(w http.ResponseWriter, err error) {
	slog.Default().Error("internal server error", "err", err)
	if debugMode.Load() {
		JSON(w, http.StatusInternalServerError, map[string]any{
			"error":  "internal server error",
			"detail": err.Error(),
		})
		return
	}
	Error(w, http.StatusInternalServerError, "internal server error")
}
