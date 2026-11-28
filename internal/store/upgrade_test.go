package store

import (
	"context"
	"path/filepath"
	"testing"
)

// An upgrade must preserve both user-owned library data and unrelated tables.
func TestMangaStartupPreservesExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "backend.db")
	first, err := OpenBackend(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := first.CreateUser(ctx, "reader", []byte("existing-password-hash"), "learner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.AddLibrary(ctx, u.ID, "naruto"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.db.ExecContext(ctx, `UPDATE user_library SET current_chapter=12, status='reading', version=4;
CREATE TABLE historical_records (value TEXT);
INSERT INTO historical_records VALUES ('preserve me');`); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenBackend(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	loaded, err := second.FindUserByUsername(ctx, "reader")
	if err != nil || loaded.ID != u.ID || string(loaded.PasswordHash) != "existing-password-hash" {
		t.Fatalf("account after upgrade: %v, %v", loaded, err)
	}
	e, err := second.GetLibraryEntry(ctx, loaded.ID, "naruto")
	if err != nil || e.CurrentChapter != 12 || e.Status != "reading" || e.Version != 4 {
		t.Fatalf("library after upgrade: %v, %v", e, err)
	}
	var historical string
	if err := second.db.QueryRowContext(ctx, "SELECT value FROM historical_records").Scan(&historical); err != nil || historical != "preserve me" {
		t.Fatalf("historical data: %q, %v", historical, err)
	}
}
