package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/romangolovachev/selfcare/pkg/respond"
)

// Recoverer catches panics, logs them with a full stack trace, records the event
// to error_log, and returns a 500 response in the appropriate format for the
// current environment (debug: with detail+stack; production: generic + request_id).
func Recoverer(log *slog.Logger, db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					reqID := chimw.GetReqID(r.Context())
					msg := fmt.Sprintf("%v", rec)

					log.Error("panic recovered",
						"request_id", reqID,
						"method", r.Method,
						"path", r.URL.Path,
						"panic", msg,
						"stack", stack,
					)

					_, _ = db.Exec(context.Background(),
						`INSERT INTO error_log (method, path, status, request_id, ip, user_agent, error_message)
						 VALUES ($1, $2, 500, $3, $4, $5, $6)`,
						r.Method, r.URL.Path, reqID,
						r.RemoteAddr, r.UserAgent(), msg,
					)

					if respond.IsDebug() {
						respond.JSON(w, http.StatusInternalServerError, map[string]any{
							"error":      "panic",
							"detail":     msg,
							"stack":      stack,
							"request_id": reqID,
						})
						return
					}
					respond.JSON(w, http.StatusInternalServerError, map[string]any{
						"error":      "internal server error",
						"request_id": reqID,
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
