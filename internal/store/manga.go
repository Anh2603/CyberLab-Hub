package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrMangaNotFound = errors.New("manga not found")
var ErrLibraryNotFound = errors.New("library entry not found")
var ErrLibraryExists = errors.New("manga already in library")
var ErrInvalidChapters = errors.New("chapter count must increase")

type Manga struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Author        string   `json:"author"`
	Genres        []string `json:"genres"`
	Status        string   `json:"status"`
	TotalChapters int      `json:"total_chapters"`
	Description   string   `json:"description"`
}
type LibraryEntry struct {
	MangaID        string    `json:"manga_id"`
	Status         string    `json:"status"`
	CurrentChapter int       `json:"current_chapter"`
	Version        int64     `json:"version"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *Backend) MigrateLibrary(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS user_library (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 manga_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('plan-to-read','reading','completed')),
 current_chapter INTEGER NOT NULL DEFAULT 0 CHECK(current_chapter>=0),
 version INTEGER NOT NULL DEFAULT 1 CHECK(version>=1),
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(user_id,manga_id));`)
	return err
}
func (s *Catalog) MigrateManga(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS manga (
 id TEXT PRIMARY KEY,title TEXT NOT NULL,author TEXT NOT NULL,genres TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('ongoing','completed','hiatus')),
 total_chapters INTEGER NOT NULL CHECK(total_chapters>=0),description TEXT NOT NULL);`)
	return err
}
func (s *Catalog) SeedManga(ctx context.Context, seeds []Manga) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM manga").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, m := range seeds {
		genres, _ := json.Marshal(m.Genres)
		if _, err = tx.ExecContext(ctx, "INSERT INTO manga(id,title,author,genres,status,total_chapters,description) VALUES(?,?,?,?,?,?,?)", m.ID, m.Title, m.Author, string(genres), m.Status, m.TotalChapters, m.Description); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func scanManga(row rowScanner) (Manga, error) {
	var m Manga
	var genres string
	err := row.Scan(&m.ID, &m.Title, &m.Author, &genres, &m.Status, &m.TotalChapters, &m.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrMangaNotFound
	}
	if err != nil {
		return m, err
	}
	err = json.Unmarshal([]byte(genres), &m.Genres)
	return m, err
}
func (s *Catalog) GetManga(ctx context.Context, id string) (Manga, error) {
	return scanManga(s.db.QueryRowContext(ctx, "SELECT id,title,author,genres,status,total_chapters,description FROM manga WHERE id=?", id))
}
func (s *Catalog) SearchManga(ctx context.Context, q string) ([]Manga, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,author,genres,status,total_chapters,description FROM manga WHERE title LIKE ? ESCAPE '\' ORDER BY title,id`, "%"+escapeLike(strings.TrimSpace(q))+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Manga{}
	for rows.Next() {
		m, err := scanManga(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
func (s *Catalog) UpdateChapters(ctx context.Context, id string, total int) (Manga, error) {
	r, err := s.db.ExecContext(ctx, "UPDATE manga SET total_chapters=? WHERE id=? AND total_chapters<?", total, id, total)
	if err != nil {
		return Manga{}, err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		if _, err := s.GetManga(ctx, id); err != nil {
			return Manga{}, err
		}
		return Manga{}, ErrInvalidChapters
	}
	return s.GetManga(ctx, id)
}
func (s *Backend) AddLibrary(ctx context.Context, userID int64, id string) (LibraryEntry, error) {
	_, err := s.db.ExecContext(ctx, "INSERT INTO user_library(user_id,manga_id,status) VALUES(?,?,'plan-to-read')", userID, id)
	if err != nil {
		if isConstraintError(err) {
			return LibraryEntry{}, ErrLibraryExists
		}
		return LibraryEntry{}, err
	}
	return s.GetLibraryEntry(ctx, userID, id)
}
func scanLibrary(row rowScanner) (LibraryEntry, error) {
	var e LibraryEntry
	err := row.Scan(&e.MangaID, &e.Status, &e.CurrentChapter, &e.Version, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrLibraryNotFound
	}
	return e, err
}
func (s *Backend) GetLibraryEntry(ctx context.Context, userID int64, id string) (LibraryEntry, error) {
	return scanLibrary(s.db.QueryRowContext(ctx, "SELECT manga_id,status,current_chapter,version,updated_at FROM user_library WHERE user_id=? AND manga_id=?", userID, id))
}
func (s *Backend) ListLibrary(ctx context.Context, userID int64) ([]LibraryEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT manga_id,status,current_chapter,version,updated_at FROM user_library WHERE user_id=? ORDER BY manga_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []LibraryEntry{}
	for rows.Next() {
		e, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (s *Backend) UpdateLibrary(ctx context.Context, userID int64, id, status string, chapter int) (LibraryEntry, error) {
	if chapter < 0 || (status != "plan-to-read" && status != "reading" && status != "completed") {
		return LibraryEntry{}, fmt.Errorf("invalid library progress")
	}
	// UPDATE RETURNING captures this write's version before any concurrent write.
	return scanLibrary(s.db.QueryRowContext(ctx, `UPDATE user_library SET status=?, current_chapter=?, version=version+1, updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND manga_id=? RETURNING manga_id,status,current_chapter,version,updated_at`, status, chapter, userID, id))
}
