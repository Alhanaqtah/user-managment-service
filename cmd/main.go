package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"user-management-service/internal/broker/rabbitmq"
	"user-management-service/internal/cache/redis"
	"user-management-service/internal/config"
	authhandler "user-management-service/internal/http-server/handlers/auth"
	"user-management-service/internal/http-server/handlers/healthcheck"
	userhabdler "user-management-service/internal/http-server/handlers/user"
	"user-management-service/internal/lib/logger"
	"user-management-service/internal/lib/logger/sl"
	authservice "user-management-service/internal/service/auth"
	userservice "user-management-service/internal/service/user"
	"user-management-service/internal/storage/postgres"

	"github.com/go-chi/chi"
)

func main() {
	cfg := config.MustLoad()

	log := logger.New(cfg.Env)

	log.Info("initializing server...", slog.String("addr", cfg.Address))

	// Storage
	storage, err := postgres.New(cfg.Storage)
	if err != nil {
		log.Error("failed to init storage", sl.Error(err))
		os.Exit(1)
	}
	log.Debug("storage initialized")

	// Cache
	cache, err := redis.New(cfg.Cache)
	if err != nil {
		log.Error("failed to init cache", sl.Error(err))
		os.Exit(1)
	}
	log.Debug("cache initialized")

	// Broker
	broker, err := rabbitmq.New(cfg.Broker)
	if err != nil {
		log.Error("failed to init message broker", sl.Error(err))
		os.Exit(1)
	}
	log.Debug("broker initialized")

	// Service layer
	authService := authservice.New(log, storage, cache, broker, cfg.Token)
	userService := userservice.New(log, storage)

	// Constroller layer
	r := chi.NewRouter()

	// r.Use(middleware.RequestID)
	// r.Use(middleware.RealIP)
	// r.Use(middleware.Logger)
	// r.Use(middleware.Recoverer)

	auth := authhandler.New(log, authService, cfg.Token)
	user := userhabdler.New(log, userService, cfg.Token)

	r.HandleFunc("/healthcheck", healthcheck.Register())
	r.Route("/auth", auth.Register())
	r.Route("/users", user.Register())

	// Server
	srv := http.Server{
		Handler:      r,
		Addr:         cfg.Address,
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	log.Debug("server initialized")
	log.Info("server is running...")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Error("failed to start server", sl.Error(err))
		}
	}()

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout*time.Second)
	defer cancel()

	srv.Shutdown(ctx)

	log.Info("server stopped")
}
