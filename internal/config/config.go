package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultHTTPAddr       = ":8080"
	defaultTCPAddr        = ":9090"
	defaultUDPAddr        = ":9091"
	defaultCatalogAddr    = ":9092"
	defaultCatalogTarget  = "localhost:9092"
	defaultBackendURL     = "http://localhost:8080"
	defaultShutdownPeriod = 5 * time.Second
	defaultGRPCTimeout    = 2 * time.Second
)

type Backend struct {
	HTTPAddr          string
	TCPAddr           string
	UDPAddr           string
	CatalogGRPCAddr   string
	JWTSecret         string
	GRPCTimeout       time.Duration
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
}

type Catalog struct {
	ListenAddr      string
	ShutdownTimeout time.Duration
}

type TUI struct {
	BackendHTTPURL string
	BackendTCPAddr string
	BackendUDPAddr string
}

func LoadBackend() (Backend, error) {
	grpcTimeout, err := durationFromEnv("GRPC_TIMEOUT", defaultGRPCTimeout)
	if err != nil {
		return Backend{}, err
	}
	shutdownTimeout, err := durationFromEnv("SHUTDOWN_TIMEOUT", defaultShutdownPeriod)
	if err != nil {
		return Backend{}, err
	}
	readHeaderTimeout, err := durationFromEnv("HTTP_READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Backend{}, err
	}

	cfg := Backend{
		HTTPAddr:          envOr("HTTP_ADDR", defaultHTTPAddr),
		TCPAddr:           envOr("TCP_ADDR", defaultTCPAddr),
		UDPAddr:           envOr("UDP_ADDR", defaultUDPAddr),
		CatalogGRPCAddr:   envOr("CATALOG_GRPC_ADDR", defaultCatalogTarget),
		JWTSecret:         strings.TrimSpace(os.Getenv("JWT_SECRET")),
		GRPCTimeout:       grpcTimeout,
		ShutdownTimeout:   shutdownTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	if err := validateListenAddress("HTTP_ADDR", cfg.HTTPAddr); err != nil {
		return Backend{}, err
	}
	if err := validateListenAddress("TCP_ADDR", cfg.TCPAddr); err != nil {
		return Backend{}, err
	}
	if err := validateListenAddress("UDP_ADDR", cfg.UDPAddr); err != nil {
		return Backend{}, err
	}
	if err := validateTargetAddress("CATALOG_GRPC_ADDR", cfg.CatalogGRPCAddr); err != nil {
		return Backend{}, err
	}
	if len(cfg.JWTSecret) < 32 {
		return Backend{}, errors.New("JWT_SECRET must contain at least 32 characters")
	}
	return cfg, nil
}

func LoadCatalog() (Catalog, error) {
	shutdownTimeout, err := durationFromEnv("SHUTDOWN_TIMEOUT", defaultShutdownPeriod)
	if err != nil {
		return Catalog{}, err
	}
	cfg := Catalog{
		ListenAddr:      envOr("CATALOG_LISTEN_ADDR", defaultCatalogAddr),
		ShutdownTimeout: shutdownTimeout,
	}
	if err := validateListenAddress("CATALOG_LISTEN_ADDR", cfg.ListenAddr); err != nil {
		return Catalog{}, err
	}
	return cfg, nil
}

func LoadTUI() (TUI, error) {
	cfg := TUI{
		BackendHTTPURL: envOr("BACKEND_HTTP_URL", defaultBackendURL),
		BackendTCPAddr: envOr("BACKEND_TCP_ADDR", "localhost"+defaultTCPAddr),
		BackendUDPAddr: envOr("BACKEND_UDP_ADDR", "localhost"+defaultUDPAddr),
	}
	parsed, err := url.ParseRequestURI(cfg.BackendHTTPURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return TUI{}, fmt.Errorf("BACKEND_HTTP_URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return TUI{}, fmt.Errorf("BACKEND_HTTP_URL must use http or https")
	}
	if err := validateTargetAddress("BACKEND_TCP_ADDR", cfg.BackendTCPAddr); err != nil {
		return TUI{}, err
	}
	if err := validateTargetAddress("BACKEND_UDP_ADDR", cfg.BackendUDPAddr); err != nil {
		return TUI{}, err
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func validateListenAddress(name, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("%s must be a valid host:port listen address", name)
	}
	return nil
}

func validateTargetAddress(name, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return fmt.Errorf("%s must be a valid host:port target", name)
	}
	return nil
}
