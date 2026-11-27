package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentChapterReleasesReturnOwnCommittedValue(t *testing.T) {
	ctx := context.Background()
	s, err := OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	seed := Manga{ID: "release-test", Title: "Release Test", Author: "Test Author", Genres: []string{"Test"}, Status: "ongoing", TotalChapters: 1, Description: "Test metadata"}
	if err := s.SeedManga(ctx, []Manga{seed}); err != nil {
		t.Fatal(err)
	}
	const last = 101
	start := make(chan struct{})
	var wg sync.WaitGroup
	for chapter := 2; chapter <= last; chapter++ {
		wg.Add(1)
		go func(chapter int) {
			defer wg.Done()
			<-start
			m, err := s.UpdateChapters(ctx, seed.ID, chapter)
			if errors.Is(err, ErrInvalidChapters) {
				return
			} // A higher release may commit first.
			if err != nil {
				t.Errorf("release %d: %v", chapter, err)
				return
			}
			if m.ID != seed.ID || m.Title != seed.Title || m.TotalChapters != chapter {
				t.Errorf("release %d returned another write's metadata: %+v", chapter, m)
			}
		}(chapter)
	}
	close(start)
	wg.Wait()
	m, err := s.GetManga(ctx, seed.ID)
	if err != nil || m.TotalChapters != last {
		t.Fatalf("final catalog = %+v, error = %v", m, err)
	}
	if _, err := s.UpdateChapters(ctx, seed.ID, last); !errors.Is(err, ErrInvalidChapters) {
		t.Fatalf("repeated release: %v", err)
	}
	if _, err := s.UpdateChapters(ctx, seed.ID, last-1); !errors.Is(err, ErrInvalidChapters) {
		t.Fatalf("decreasing release: %v", err)
	}
	if _, err := s.UpdateChapters(ctx, "missing", last); !errors.Is(err, ErrMangaNotFound) {
		t.Fatalf("unknown manga: %v", err)
	}
}
