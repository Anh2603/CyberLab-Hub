package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadBackendDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	cfg, err := LoadBackend()
	if err != nil {
		t.Fatalf("LoadBackend() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.CatalogGRPCAddr != "localhost:9092" {
		t.Fatalf("CatalogGRPCAddr = %q, want %q", cfg.CatalogGRPCAddr, "localhost:9092")
	}
	if cfg.GRPCTimeout != 2*time.Second {
		t.Fatalf("GRPCTimeout = %v, want %v", cfg.GRPCTimeout, 2*time.Second)
	}
}

func TestLoadBackendRejectsWeakJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "short")

	_, err := LoadBackend()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("LoadBackend() error = %v, want JWT_SECRET validation error", err)
	}
}

func TestLoadBackendRejectsInvalidDuration(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	t.Setenv("GRPC_TIMEOUT", "eventually")

	_, err := LoadBackend()
	if err == nil || !strings.Contains(err.Error(), "GRPC_TIMEOUT") {
		t.Fatalf("LoadBackend() error = %v, want GRPC_TIMEOUT validation error", err)
	}
}

func TestLoadTUIRejectsRelativeBackendURL(t *testing.T) {
	t.Setenv("BACKEND_HTTP_URL", "/api")

	_, err := LoadTUI()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_HTTP_URL") {
		t.Fatalf("LoadTUI() error = %v, want BACKEND_HTTP_URL validation error", err)
	}
}
