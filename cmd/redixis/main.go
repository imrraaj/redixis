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

	"redixis/internal/config"
	"redixis/internal/httpapi"
	"redixis/internal/observability"
	"redixis/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	metrics := observability.NewPrometheus("redixis")

	authRedis := store.NewRedisClient(cfg.AuthRedisURL)
	defer authRedis.Close()

	tenantRedis := store.NewRedisClient(cfg.TenantRedisURL)
	defer tenantRedis.Close()

	router := httpapi.NewRouter(httpapi.Dependencies{
		Config:      cfg,
		Logger:      logger,
		Metrics:     metrics,
		AuthRedis:   authRedis,
		TenantRedis: tenantRedis,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("server_starting", "addr", cfg.HTTPAddr, "env", cfg.Environment)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	logger.Info("server_stopping", "timeout", cfg.ShutdownTimeout.String())
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server_shutdown_failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server_stopped")
}
