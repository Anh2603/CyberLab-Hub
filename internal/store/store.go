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
	ErrUserNotFound      = errors.New("user not found")
	ErrUserExists        = errors.New("user already exists")
	ErrChallengeNotFound = errors.New("challenge not found")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash []byte
	Role         string
	CreatedAt    time.Time
}

type Progress struct {
	ChallengeID string
	SolvedAt    time.Time
}

type Backend struct{ db *sql.DB }

type Catalog struct{ db *sql.DB }

type Challenge struct {
	ID          string
	Title       string
	Description string
	Category    string
	Difficulty  string
	AnswerHash  []byte
	Published   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

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
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate catalog database: %w", err)
	}
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
);
CREATE TABLE IF NOT EXISTS submissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge_id TEXT NOT NULL,
    correct INTEGER NOT NULL CHECK (correct IN (0, 1)),
    submitted_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS progress (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    challenge_id TEXT NOT NULL,
    solved_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, challenge_id)
);`)
	return err
}

func (s *Catalog) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS challenges (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    category TEXT NOT NULL,
    difficulty TEXT NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    answer_hash BLOB NOT NULL,
    published INTEGER NOT NULL DEFAULT 0 CHECK (published IN (0, 1)),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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

func (s *Backend) RecordSubmission(ctx context.Context, userID int64, challengeID string, correct bool) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin submission: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO submissions (user_id, challenge_id, correct) VALUES (?, ?, ?)", userID, challengeID, correct); err != nil {
		return fmt.Errorf("insert submission: %w", err)
	}
	if correct {
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO progress (user_id, challenge_id) VALUES (?, ?)
ON CONFLICT(user_id, challenge_id) DO NOTHING`, userID, challengeID); err != nil {
			return fmt.Errorf("record progress: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit submission: %w", err)
	}
	return nil
}

func (s *Backend) ListProgress(ctx context.Context, userID int64) ([]Progress, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT challenge_id, solved_at FROM progress WHERE user_id = ? ORDER BY solved_at, challenge_id", userID)
	if err != nil {
		return nil, fmt.Errorf("query progress: %w", err)
	}
	defer rows.Close()
	var progress []Progress
	for rows.Next() {
		var item Progress
		if err := rows.Scan(&item.ChallengeID, &item.SolvedAt); err != nil {
			return nil, fmt.Errorf("scan progress: %w", err)
		}
		progress = append(progress, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate progress: %w", err)
	}
	return progress, nil
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

// Upgrade only placeholder text shipped previously, preserving custom content,
// answer hashes and publication state.
func (s *Catalog) UpdateLegacyDescription(ctx context.Context, id, previous, replacement string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE challenges SET description = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND description = ?", replacement, id, previous)
	return err
}

// Correct the difficulty only when the bundled exercise still matches its
// original title, description, category, and difficulty.
func (s *Catalog) UpdateSeedDifficulty(ctx context.Context, id, title, description, category, previous, replacement string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE challenges SET difficulty = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND title = ? AND description = ? AND category = ? AND difficulty = ?`,
		replacement, id, title, description, category, previous)
	return err
}

func (s *Catalog) ChallengeCount(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM challenges").Scan(&count); err != nil {
		return 0, fmt.Errorf("count challenges: %w", err)
	}
	return count, nil
}

func (s *Catalog) PublishChallenge(ctx context.Context, challenge Challenge) (Challenge, error) {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO challenges (id, title, description, category, difficulty, answer_hash, published)
VALUES (?, ?, ?, ?, ?, ?, 1)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    description = excluded.description,
    category = excluded.category,
    difficulty = excluded.difficulty,
    answer_hash = excluded.answer_hash,
    published = 1,
    updated_at = CURRENT_TIMESTAMP`,
		challenge.ID, challenge.Title, challenge.Description, challenge.Category,
		challenge.Difficulty, challenge.AnswerHash)
	if err != nil {
		return Challenge{}, fmt.Errorf("publish challenge: %w", err)
	}
	return s.GetChallenge(ctx, challenge.ID, true)
}

func (s *Catalog) ArchiveChallenge(ctx context.Context, id string) (Challenge, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE challenges SET published = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	if err != nil {
		return Challenge{}, fmt.Errorf("archive challenge: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Challenge{}, fmt.Errorf("read archive result: %w", err)
	}
	if affected == 0 {
		return Challenge{}, ErrChallengeNotFound
	}
	return s.GetChallenge(ctx, id, true)
}

func (s *Catalog) GetChallenge(ctx context.Context, id string, includeUnpublished bool) (Challenge, error) {
	query := `SELECT id, title, description, category, difficulty, answer_hash, published, created_at, updated_at
FROM challenges WHERE id = ?`
	if !includeUnpublished {
		query += " AND published = 1"
	}
	return scanChallenge(s.db.QueryRowContext(ctx, query, id))
}

func (s *Catalog) SearchChallenges(ctx context.Context, text, category, difficulty string) ([]Challenge, error) {
	query := `SELECT id, title, description, category, difficulty, answer_hash, published, created_at, updated_at
FROM challenges WHERE published = 1`
	args := make([]any, 0, 3)
	if text != "" {
		query += ` AND (title LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')`
		pattern := "%" + escapeLike(text) + "%"
		args = append(args, pattern, pattern)
	}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	if difficulty != "" {
		query += " AND difficulty = ?"
		args = append(args, difficulty)
	}
	query += " ORDER BY difficulty, title, id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search challenges: %w", err)
	}
	defer rows.Close()
	var challenges []Challenge
	for rows.Next() {
		challenge, err := scanChallenge(rows)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, challenge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate challenges: %w", err)
	}
	return challenges, nil
}

func scanChallenge(row rowScanner) (Challenge, error) {
	var challenge Challenge
	if err := row.Scan(&challenge.ID, &challenge.Title, &challenge.Description, &challenge.Category,
		&challenge.Difficulty, &challenge.AnswerHash, &challenge.Published,
		&challenge.CreatedAt, &challenge.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Challenge{}, ErrChallengeNotFound
		}
		return Challenge{}, fmt.Errorf("scan challenge: %w", err)
	}
	return challenge, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
