package main

import (
	"context"
	"log/slog" // Import slog
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/noahxzhu/pushover-notify/internal/config"
	"github.com/noahxzhu/pushover-notify/internal/storage"
	"github.com/noahxzhu/pushover-notify/internal/web"
	"github.com/noahxzhu/pushover-notify/internal/worker"
)

func main() {
	// Setup structured logger (JSON handler)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Load Config
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Init Storage
	store := storage.NewStore(cfg.Storage.FilePath)
	if err := store.Load(); err != nil {
		slog.Error("Failed to load storage", "error", err)
		os.Exit(1)
	}

	// Init Worker
	w := worker.NewWorker(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start Worker
	go w.Start(ctx)

	// Init Web Server
	srv := web.NewServer(store, w)
	httpServer := &http.Server{
		Addr:    cfg.Server.Port,
		Handler: srv,
	}

	// Start HTTP Server
	go func() {
		slog.Info("Starting server", "port", cfg.Server.Port, "url", "http://localhost"+cfg.Server.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down...")
	cancel() // Stop worker

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("Server exited")
}
