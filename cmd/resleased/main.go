package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"resleased/internal/api"
	"resleased/internal/store"
)

var version = "dev" // overridden by -ldflags at build time
var commit_sha = "dev"

// @title					resleased
// @version				VERSION_PLACEHOLDER
// @description		A lightweight HTTP service for exclusive, time-bounded resource reservations.
// @license.name	AGPL-3.0
// @license.url		https://www.gnu.org/licenses/agpl-3.0.html
// @host					localhost:8080
// @BasePath			/
func main() {
	addr := flag.String("addr", ":8080", "listen address")
	stateFile := flag.String("state", "resleased.json", "path to state persistence file")
	purgeInt := flag.Duration("purge-interval", 5*time.Minute, "how often to purge expired reservations from state file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	slog.Info("starting resleased", "version", version, "commit", commit_sha)

	s, err := store.New(*stateFile)
	if err != nil {
		slog.Error("failed to initialise store", "err", err)
		os.Exit(1)
	}
	slog.Info("state loaded", "file", *stateFile)

	// Periodically remove expired entries so the state file stays clean.
	go func() {
		ticker := time.NewTicker(*purgeInt)
		defer ticker.Stop()
		for range ticker.C {
			if err := s.Purge(); err != nil {
				slog.Error("purge failed", "err", err)
			} else {
				slog.Debug("expired reservations purged")
			}
		}
	}()

	handler := api.New(s)
	srv := &http.Server{
		Addr:         *addr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("resleased listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down…")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
	slog.Info("stopped")
}
