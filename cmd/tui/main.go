package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Anh2603/CyberLab-Hub/internal/config"
	"github.com/rivo/tview"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.LoadTUI()
	if err != nil {
		logger.Error("invalid TUI configuration", "error", err)
		os.Exit(1)
	}

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	status.SetBorder(true).SetTitle(" CyberLab Hub ")
	status.SetText(fmt.Sprintf(
		"[green]Phase 1 foundation is running[-]\n\nBackend: %s\nTCP: %s\nUDP: %s\n\nPress Ctrl+C to exit.",
		cfg.BackendHTTPURL,
		cfg.BackendTCPAddr,
		cfg.BackendUDPAddr,
	))

	if err := tview.NewApplication().SetRoot(status, true).Run(); err != nil {
		logger.Error("TUI stopped", "error", err)
		os.Exit(1)
	}
}
