// @title           Self-Care API
// @version         1.0
// @description     Backend API для приложения Self-Care
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization

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
	"github.com/robfig/cron/v3"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/romangolovachev/selfcare/internal/admin"
	"github.com/romangolovachev/selfcare/internal/analysis"
	"github.com/romangolovachev/selfcare/pkg/respond"
	"github.com/romangolovachev/selfcare/internal/auth"
	"github.com/romangolovachev/selfcare/internal/liveresponse"
	"github.com/romangolovachev/selfcare/internal/migrate"
	"github.com/romangolovachev/selfcare/internal/mood"
	"github.com/romangolovachev/selfcare/internal/notification"
	"github.com/romangolovachev/selfcare/internal/payments"
	"github.com/romangolovachev/selfcare/internal/personalinsights"
	"github.com/romangolovachev/selfcare/internal/subscription"
	"github.com/romangolovachev/selfcare/internal/user"
	"github.com/romangolovachev/selfcare/internal/weeklycard"
	"github.com/romangolovachev/selfcare/pkg/database"
	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	apimiddleware "github.com/romangolovachev/selfcare/pkg/middleware"
	_ "github.com/romangolovachev/selfcare/docs"
)

func main() {
	_ = godotenv.Load()

	respond.SetDebugMode(os.Getenv("APP_ENV") == "preview")

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := database.Connect(os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := migrate.Run(context.Background(), db); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}

	jwtSvc := jwtutil.New(os.Getenv("JWT_SECRET"))

	authRepo := auth.NewRepository(db)
	authSvc := auth.NewService(authRepo, jwtSvc)
	authHandler := auth.NewHandler(authSvc)

	userRepo := user.NewRepository(db)
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userSvc)

	notifRepo := notification.NewRepository(db)
	notifSvc := notification.NewService(db, notifRepo)
	notifHandler := notification.NewHandler(notifSvc)

	moodRepo := mood.NewRepository(db)
	moodSvc := mood.NewService(moodRepo, db, notifSvc)
	moodHandler := mood.NewHandler(moodSvc)

	analysisSvc := analysis.NewService(db, notifSvc)
	analysisHandler := analysis.NewHandler(analysisSvc)

	piSvc := personalinsights.NewService(db)

	lrClient := liveresponse.NewOpenRouterClient(
		os.Getenv("OPENROUTER_API_KEY"),
		getenv("DEEPSEEK_MODEL", "deepseek/deepseek-v4-pro"),
	)
	lrSvc := liveresponse.NewService(db, lrClient)
	lrHandler := liveresponse.NewHandler(lrSvc)

	subSvc := subscription.NewService(db)
	subHandler := subscription.NewHandler(subSvc)

	paymentsSvc := payments.NewService(
		db,
		os.Getenv("PRODAMUS_PAYFORM_URL"),
		os.Getenv("PRODAMUS_SECRET_KEY"),
		getenv("PRODAMUS_SUCCESS_URL", "selfcare://payment-success"),
	)
	paymentsHandler := payments.NewHandler(paymentsSvc)

	wcSvc := weeklycard.NewService(db)
	wcHandler := weeklycard.NewHandler(wcSvc)

	adminRepo := admin.NewRepository(db)
	adminHandler := admin.NewHandler(adminRepo, notifSvc, analysisSvc, wcSvc, lrSvc)

	// Moscow is UTC+3 and has no DST since 2014 — fixed zone is correct
	moscow := time.FixedZone("MSK", 3*60*60)
	c := cron.New(cron.WithLocation(moscow))
	if _, err := c.AddFunc("0 20 * * 5", func() {
		log.Info("insights: weekly regen started")
		analysisSvc.RegenerateAll(context.Background())
		log.Info("insights: weekly regen done")
	}); err != nil {
		log.Error("cron: register insights job", "err", err)
		os.Exit(1)
	}
	if _, err := c.AddFunc("0 21 * * *", func() {
		log.Info("notifications: daily reminder started")
		notifSvc.SendDailyReminders(context.Background())
		log.Info("notifications: daily reminder done")
	}); err != nil {
		log.Error("cron: register daily reminder", "err", err)
		os.Exit(1)
	}
	if _, err := c.AddFunc("0 3 * * *", func() {
		log.Info("personalinsights: daily compute started")
		ctx := context.Background()
		piSvc.FillNextDayMoods(ctx)
		piSvc.ComputeAll(ctx)
		log.Info("personalinsights: daily compute done")
	}); err != nil {
		log.Error("cron: register personalinsights job", "err", err)
		os.Exit(1)
	}
	// Weekly card: every Sunday at 20:00 MSK
	if _, err := c.AddFunc("0 20 * * 0", func() {
		log.Info("weeklycard: regen started")
		wcSvc.ComputeAll(context.Background())
		log.Info("weeklycard: regen done")
	}); err != nil {
		log.Error("cron: register weeklycard job", "err", err)
		os.Exit(1)
	}
	c.Start()
	defer c.Stop()

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(apimiddleware.Recoverer(log, db))
	r.Use(chimw.Timeout(85 * time.Second))
	r.Use(corsMiddleware)
	r.Use(apimiddleware.ErrorLogger(db))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})
	r.Get("/docs/*", httpSwagger.WrapHandler)

	r.Route("/api/v1", func(r chi.Router) {
		r.Mount("/auth", authHandler.Routes(jwtSvc))

		// Продамус webhook — no JWT (Продамус calls it directly)
		r.Mount("/payments/webhook", paymentsHandler.WebhookRoutes())

		r.Group(func(r chi.Router) {
			r.Use(jwtSvc.MiddlewareWithVersionCheck(authSvc.VersionChecker()))
			r.Mount("/users", userHandler.Routes())
			r.Mount("/moods", moodHandler.Routes())
			r.Mount("/analysis", analysisHandler.Routes())
			r.Mount("/notifications", notifHandler.Routes())
			r.Mount("/subscription", subHandler.Routes())
			r.Mount("/weekly-card", wcHandler.Routes())
			r.Mount("/payments/create", paymentsHandler.CreateRoutes())
			r.Mount("/admin", adminHandler.Routes())
			r.Group(func(r chi.Router) {
				r.Use(subSvc.LRAccessMiddleware)
				r.Mount("/live-response", lrHandler.Routes())
			})
		})
	})

	addr := ":" + getenv("PORT", "8080")
	srv := &http.Server{Addr: addr, Handler: r, ReadTimeout: 10 * time.Second, WriteTimeout: 90 * time.Second}

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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
