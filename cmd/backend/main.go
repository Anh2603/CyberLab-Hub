package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/backend"
	"github.com/Anh2603/CyberLab-Hub/internal/config"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.LoadBackend()
	if err != nil {
		logger.Error("invalid backend configuration", "error", err)
		os.Exit(1)
	}
	accounts, err := store.OpenBackend(context.Background(), cfg.DatabasePath)
	if err != nil {
		logger.Error("backend database initialization failed", "error", err)
		os.Exit(1)
	}
	defer accounts.Close()

	created, err := backend.BootstrapAdmin(context.Background(), accounts, cfg.BootstrapAdmin, cfg.BootstrapPassword)
	if err != nil {
		logger.Error("administrator bootstrap failed", "error", err)
		os.Exit(1)
	}
	if created {
		logger.Info("administrator account created", "username", cfg.BootstrapAdmin)
	}
	tokens := auth.NewManager(cfg.JWTSecret, cfg.JWTTTL)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           backend.NewRouter(backend.Dependencies{Accounts: accounts, Tokens: tokens}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("backend listening", "http_addr", cfg.HTTPAddr)
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("backend shutdown requested")
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("backend stopped unexpectedly", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("backend graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("backend stopped")
}
