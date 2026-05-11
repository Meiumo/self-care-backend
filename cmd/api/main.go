package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"github.com/romangolovachev/selfcare/internal/analysis"
	"github.com/romangolovachev/selfcare/internal/auth"
	"github.com/romangolovachev/selfcare/internal/mood"
	"github.com/romangolovachev/selfcare/internal/user"
	"github.com/romangolovachev/selfcare/pkg/database"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
)

func main() {
	_ = godotenv.Load()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := database.Connect(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	jwtSvc := jwtutil.New(os.Getenv("JWT_SECRET"))

	authRepo := auth.NewRepository(db)
	authSvc := auth.NewService(authRepo, jwtSvc)
	authHandler := auth.NewHandler(authSvc)

	userRepo := user.NewRepository(db)
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userSvc)

	moodRepo := mood.NewRepository(db)
	moodSvc := mood.NewService(moodRepo)
	moodHandler := mood.NewHandler(moodSvc)

	analysisSvc := analysis.NewService(os.Getenv("CLAUDE_API_KEY"))
	analysisHandler := analysis.NewHandler(analysisSvc)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))
	r.Use(corsMiddleware)

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Mount("/auth", authHandler.Routes())

		r.Group(func(r chi.Router) {
			r.Use(jwtSvc.Middleware)
			r.Mount("/users", userHandler.Routes())
			r.Mount("/moods", moodHandler.Routes())
			r.Mount("/analysis", analysisHandler.Routes())
		})
	})

	addr := ":" + getenv("PORT", "8080")
	srv := &http.Server{Addr: addr, Handler: r, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}

	go func() {
		log.Info("server started", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Info("server stopped")
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
