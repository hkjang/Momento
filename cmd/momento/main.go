package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hkjang/Momento/internal/auth"
	"github.com/hkjang/Momento/internal/config"
	"github.com/hkjang/Momento/internal/database"
	"github.com/hkjang/Momento/internal/httpapi"
	"github.com/hkjang/Momento/internal/service"
	"github.com/hkjang/Momento/internal/version"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration rejected", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	db, err := database.Open(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	authService := auth.Service{DB: db}
	if err := authService.Bootstrap(ctx, cfg.BootstrapAdmin, cfg.BootstrapPassword); err != nil {
		logger.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
	var webFS fs.FS
	for _, dir := range []string{"/app/web", "web/dist"} {
		if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
			webFS = os.DirFS(dir)
			break
		}
	}
	api := httpapi.New(db, webFS, logger)
	worker := service.Worker{DB: db}
	go worker.Run(ctx)
	server := &http.Server{Addr: ":8080", Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		logger.Info("Momento started", "version", version.Version, "address", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("forced shutdown", "error", err)
	}
}
