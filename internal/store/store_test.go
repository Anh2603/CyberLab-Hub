package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestBackendPersistsUsersAndRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "backend.db")
	first, err := OpenBackend(ctx, path)
	if err != nil {
		t.Fatalf("OpenBackend() error = %v", err)
	}
	created, err := first.CreateUser(ctx, "alice", []byte("hash"), "learner")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := first.CreateUser(ctx, "ALICE", []byte("other"), "learner"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate CreateUser() error = %v, want ErrUserExists", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := OpenBackend(ctx, path)
	if err != nil {
		t.Fatalf("reopen backend error = %v", err)
	}
	defer second.Close()
	loaded, err := second.FindUserByUsername(ctx, "Alice")
	if err != nil {
		t.Fatalf("FindUserByUsername() error = %v", err)
	}
	if loaded.ID != created.ID || loaded.Username != "alice" || loaded.Role != "learner" {
		t.Fatalf("persisted user = %#v, want %#v", loaded, created)
	}
}

func TestCatalogMigrationIsRepeatable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "catalog.db")
	for iteration := 0; iteration < 2; iteration++ {
		catalog, err := OpenCatalog(ctx, path)
		if err != nil {
			t.Fatalf("OpenCatalog() iteration %d error = %v", iteration, err)
		}
		if err := catalog.Close(); err != nil {
			t.Fatalf("Close() iteration %d error = %v", iteration, err)
		}
	}
}
