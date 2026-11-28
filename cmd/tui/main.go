package main

import (
	"log/slog"
	"os"

	"github.com/Anh2603/MangaHub/internal/config"
	mangatui "github.com/Anh2603/MangaHub/internal/tui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.LoadTUI()
	if err != nil {
		logger.Error("invalid TUI configuration", "error", err)
		os.Exit(1)
	}

	client, err := mangatui.NewClient(cfg.BackendHTTPURL, cfg.BackendTCPAddr, cfg.BackendUDPAddr)
	if err != nil {
		logger.Error("invalid TUI client configuration", "error", err)
		os.Exit(1)
	}
	if err := mangatui.NewMangaUI(client).Run(); err != nil {
		logger.Error("TUI stopped", "error", err)
		os.Exit(1)
	}
}
