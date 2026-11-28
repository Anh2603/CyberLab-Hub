package tui

import (
	"context"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"net/http"
	"net/url"
)

func (c *Client) SearchManga(ctx context.Context, query string) ([]store.Manga, error) {
	var r struct {
		Manga []store.Manga `json:"manga"`
	}
	err := c.request(ctx, http.MethodGet, "/api/manga?q="+url.QueryEscape(query), nil, &r, true)
	return r.Manga, err
}
func (c *Client) GetManga(ctx context.Context, id string) (store.Manga, error) {
	var m store.Manga
	err := c.request(ctx, http.MethodGet, "/api/manga/"+url.PathEscape(id), nil, &m, true)
	return m, err
}
func (c *Client) Library(ctx context.Context) ([]store.LibraryEntry, error) {
	var r struct {
		Library []store.LibraryEntry `json:"library"`
	}
	err := c.request(ctx, http.MethodGet, "/api/library", nil, &r, true)
	return r.Library, err
}
func (c *Client) AddManga(ctx context.Context, id string) error {
	return c.request(ctx, http.MethodPost, "/api/library", map[string]string{"manga_id": id}, nil, true)
}
func (c *Client) UpdateReading(ctx context.Context, id, status string, chapter int) (store.LibraryEntry, error) {
	var e store.LibraryEntry
	err := c.request(ctx, http.MethodPut, "/api/library/"+url.PathEscape(id)+"/progress", map[string]any{"status": status, "current_chapter": chapter}, &e, true)
	return e, err
}
func (c *Client) ReleaseChapter(ctx context.Context, id string, total int) error {
	return c.request(ctx, http.MethodPost, "/api/admin/manga/"+url.PathEscape(id)+"/release", map[string]int{"total_chapters": total}, nil, true)
}
