package respond

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func OK(w http.ResponseWriter, v any) { JSON(w, http.StatusOK, v) }

func Created(w http.ResponseWriter, v any) { JSON(w, http.StatusCreated, v) }

func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

func BadRequest(w http.ResponseWriter, msg string) { Error(w, http.StatusBadRequest, msg) }

func Unauthorized(w http.ResponseWriter) { Error(w, http.StatusUnauthorized, "unauthorized") }

func NotFound(w http.ResponseWriter) { Error(w, http.StatusNotFound, "not found") }

func Internal(w http.ResponseWriter) { Error(w, http.StatusInternalServerError, "internal server error") }
