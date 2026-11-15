package main

import (
	"log/slog"
	"os"

	"github.com/Anh2603/CyberLab-Hub/internal/config"
	cybertui "github.com/Anh2603/CyberLab-Hub/internal/tui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.LoadTUI()
	if err != nil {
		logger.Error("invalid TUI configuration", "error", err)
		os.Exit(1)
	}

	client, err := cybertui.NewClient(cfg.BackendHTTPURL, cfg.BackendTCPAddr, cfg.BackendUDPAddr)
	if err != nil {
		logger.Error("invalid TUI client configuration", "error", err)
		os.Exit(1)
	}
	if err := cybertui.NewUI(client).Run(); err != nil {
		logger.Error("TUI stopped", "error", err)
		os.Exit(1)
	}
}
