package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash []byte
	Role         string
	CreatedAt    time.Time
}

type Backend struct{ db *sql.DB }

type Catalog struct{ db *sql.DB }

func OpenBackend(ctx context.Context, path string) (*Backend, error) {
	db, err := open(ctx, path)
	if err != nil {
		return nil, err
	}
	store := &Backend{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate backend database: %w", err)
	}
	if err := store.MigrateLibrary(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func OpenCatalog(ctx context.Context, path string) (*Catalog, error) {
	db, err := open(ctx, path)
	if err != nil {
		return nil, err
	}
	store := &Catalog{db: db}
	if err := store.MigrateManga(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func open(ctx context.Context, path string) (*sql.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		directory := filepath.Dir(path)
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure database: %w", err)
		}
	}
	return db, nil
}

func (s *Backend) Close() error { return s.db.Close() }

func (s *Catalog) Close() error { return s.db.Close() }

func (s *Backend) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash BLOB NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('learner', 'admin')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`)
	return err
}

func (s *Backend) CreateUser(ctx context.Context, username string, passwordHash []byte, role string) (User, error) {
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)", username, passwordHash, role)
	if err != nil {
		if isConstraintError(err) {
			return User{}, ErrUserExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("read inserted user id: %w", err)
	}
	return s.FindUserByID(ctx, id)
}

func (s *Backend) FindUserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?", username))
}

func (s *Backend) FindUserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?", id))
}

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (User, error) {
	var user User
	if err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &user.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return user, nil
}

func isConstraintError(err error) bool {
	var coded interface{ Code() int }
	return errors.As(err, &coded) && coded.Code()&0xff == 19
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
