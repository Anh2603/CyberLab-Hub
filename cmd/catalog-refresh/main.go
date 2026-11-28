// Explicit, idempotent migration from the original fictional classroom dataset.
package main

import (
	"context"
	"log"

	"github.com/Anh2603/CyberLab-Hub/internal/catalog"
	"github.com/Anh2603/CyberLab-Hub/internal/config"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
)

func main() {
	cfg, err := config.LoadCatalog()
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.OpenCatalog(context.Background(), cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = catalog.RefreshMangaCatalog(context.Background(), db); err != nil {
		log.Fatal(err)
	}
	items, err := db.SearchManga(context.Background(), "")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Catalog refreshed: %d visible titles. Existing library targets and chapter releases retained.", len(items))
}
