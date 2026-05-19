package middleware

import (
	"context"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrorLogger records HTTP 5xx responses to the error_log table.
func ErrorLogger(db *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if ww.Status() >= 500 {
				_, _ = db.Exec(context.Background(),
					`INSERT INTO error_log (method, path, status) VALUES ($1, $2, $3)`,
					r.Method, r.URL.Path, ww.Status(),
				)
			}
		})
	}
}
