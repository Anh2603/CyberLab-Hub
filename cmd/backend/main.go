package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/backend"
	"github.com/Anh2603/CyberLab-Hub/internal/config"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	tcpsync "github.com/Anh2603/CyberLab-Hub/internal/tcp"
	udpnotify "github.com/Anh2603/CyberLab-Hub/internal/udp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	progressServer := tcpsync.NewServer(tokens)
	notificationServer := udpnotify.NewServer(tokens)
	catalogConnection, err := grpc.NewClient(cfg.CatalogGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(1<<20),
			grpc.MaxCallSendMsgSize(1<<20),
		))
	if err != nil {
		logger.Error("catalog client initialization failed", "error", err)
		os.Exit(1)
	}
	defer catalogConnection.Close()
	catalogClient := catalogv1.NewCatalogServiceClient(catalogConnection)

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: backend.NewRouter(backend.Dependencies{
			Accounts: accounts, Tokens: tokens, Catalog: catalogClient,
			Progress: accounts, Events: progressServer, Notifier: notificationServer,
			RPCTimeout: cfg.GRPCTimeout,
		}),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErrors := make(chan error, 1)
	tcpErrors := make(chan error, 1)
	udpErrors := make(chan error, 1)
	go func() {
		logger.Info("backend listening", "http_addr", cfg.HTTPAddr)
		serveErrors <- server.ListenAndServe()
	}()
	go func() {
		logger.Info("publication notifications listening", "udp_addr", cfg.UDPAddr)
		udpErrors <- notificationServer.ListenAndServe(ctx, cfg.UDPAddr)
	}()
	go func() {
		logger.Info("progress sync listening", "tcp_addr", cfg.TCPAddr)
		tcpErrors <- progressServer.ListenAndServe(ctx, cfg.TCPAddr)
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
	case err := <-tcpErrors:
		if err != nil {
			logger.Error("progress sync stopped unexpectedly", "error", err)
			os.Exit(1)
		}
		return
	case err := <-udpErrors:
		if err != nil {
			logger.Error("publication notifications stopped unexpectedly", "error", err)
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
