package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Anh2603/CyberLab-Hub/internal/store"
)

func TestRealMangaSeedAndExplicitRefresh(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := legacyMangaSeeds()
	if err = db.SeedManga(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpdateChapters(ctx, "star-harbor", 20); err != nil {
		t.Fatal(err)
	}
	// Startup never replaces an existing dataset; replacement is an explicit action.
	if err = SeedManga(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetManga(ctx, "naruto"); err == nil {
		t.Fatal("startup inserted into nonempty catalog")
	}
	if err = RefreshMangaCatalog(ctx, db); err != nil {
		t.Fatal(err)
	}
	items, err := db.SearchManga(ctx, "")
	if err != nil || len(items) != 30 {
		t.Fatalf("catalog size %d %v", len(items), err)
	}
	genres := map[string]bool{}
	seen := map[string]bool{}
	for _, m := range items {
		if seen[m.ID] || m.Author == "" || m.Title == "" || m.Description == "" || m.TotalChapters < 1 {
			t.Fatalf("bad seed %#v", m)
		}
		seen[m.ID] = true
		for _, g := range m.Genres {
			genres[g] = true
		}
	}
	if len(genres) < 4 || !seen["naruto"] || !seen["one-piece"] {
		t.Fatal("required catalog diversity/titles missing")
	}
	old, err := db.GetManga(ctx, "star-harbor")
	if err != nil || old.TotalChapters != 20 {
		t.Fatalf("old library target lost: %#v %v", old, err)
	}
	if _, err = db.UpdateChapters(ctx, "one-piece", 1200); err != nil {
		t.Fatal(err)
	}
	if err = RefreshMangaCatalog(ctx, db); err != nil {
		t.Fatal(err)
	}
	one, err := db.GetManga(ctx, "one-piece")
	if err != nil || one.TotalChapters != 1200 {
		t.Fatal("refresh overwrote local chapter release")
	}
}
