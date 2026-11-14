package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/config"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.LoadCatalog()
	if err != nil {
		logger.Error("invalid catalog configuration", "error", err)
		os.Exit(1)
	}
	catalogStore, err := store.OpenCatalog(context.Background(), cfg.DatabasePath)
	if err != nil {
		logger.Error("catalog database initialization failed", "error", err)
		os.Exit(1)
	}
	defer catalogStore.Close()

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		logger.Error("catalog listener failed", "error", err)
		os.Exit(1)
	}

	server := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("catalog listening", "grpc_addr", cfg.ListenAddr)
		serveErrors <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		logger.Info("catalog shutdown requested")
	case err := <-serveErrors:
		logger.Error("catalog stopped unexpectedly", "error", err)
		os.Exit(1)
	}

	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	timer := time.NewTimer(cfg.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-stopped:
		logger.Info("catalog stopped")
	case <-timer.C:
		logger.Warn("catalog graceful shutdown timed out; forcing stop")
		server.Stop()
		<-stopped
	}
}
