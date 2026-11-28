package backend

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	pb "github.com/Anh2603/MangaHub/gen/catalog/v1"
	"github.com/Anh2603/MangaHub/internal/auth"
	"github.com/Anh2603/MangaHub/internal/store"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MangaClient interface {
	SearchManga(context.Context, *pb.SearchMangaRequest, ...grpc.CallOption) (*pb.SearchMangaResponse, error)
	GetManga(context.Context, *pb.GetMangaRequest, ...grpc.CallOption) (*pb.GetMangaResponse, error)
	UpdateChapters(context.Context, *pb.UpdateChaptersRequest, ...grpc.CallOption) (*pb.UpdateChaptersResponse, error)
}
type LibraryStore interface {
	AddLibrary(context.Context, int64, string) (store.LibraryEntry, error)
	ListLibrary(context.Context, int64) ([]store.LibraryEntry, error)
	UpdateLibrary(context.Context, int64, string, string, int) (store.LibraryEntry, error)
}
type MangaProgressPublisher interface {
	PublishMangaProgress(int64, string, store.LibraryEntry)
}
type ChapterNotifier interface{ NotifyNewChapter(string, string, int) }
type mangaHandler struct {
	catalog    MangaClient
	library    LibraryStore
	events     MangaProgressPublisher
	notifier   ChapterNotifier
	timeout    time.Duration
	progressMu sync.Mutex
}

func mangaResponse(m *pb.Manga) store.Manga {
	return store.Manga{ID: m.GetId(), Title: m.GetTitle(), Author: m.GetAuthor(), Genres: m.GetGenres(), Status: m.GetStatus(), TotalChapters: int(m.GetTotalChapters()), Description: m.GetDescription()}
}
func (h *mangaHandler) rpc(c *gin.Context) (context.Context, context.CancelFunc) {
	d := h.timeout
	if d <= 0 {
		d = 2 * time.Second
	}
	return context.WithTimeout(c.Request.Context(), d)
}
func (h *mangaHandler) available(c *gin.Context) bool {
	if h.catalog == nil || h.library == nil {
		writeError(c, 503, "manga service unavailable")
		return false
	}
	return true
}
func (h *mangaHandler) register(group, admin *gin.RouterGroup) {
	group.GET("/manga", h.search)
	group.GET("/manga/:id", h.get)
	group.GET("/library", h.list)
	group.POST("/library", h.add)
	group.PUT("/library/:id/progress", h.update)
	admin.POST("/manga/:id/release", h.release)
}
func (h *mangaHandler) search(c *gin.Context) {
	if !h.available(c) {
		return
	}
	ctx, cancel := h.rpc(c)
	defer cancel()
	r, err := h.catalog.SearchManga(ctx, &pb.SearchMangaRequest{Query: c.Query("q")})
	if err != nil {
		writeMangaRPCError(c, err)
		return
	}
	items := []store.Manga{}
	for _, m := range r.GetManga() {
		items = append(items, mangaResponse(m))
	}
	c.JSON(200, gin.H{"manga": items})
}
func (h *mangaHandler) get(c *gin.Context) {
	if !h.available(c) {
		return
	}
	ctx, cancel := h.rpc(c)
	defer cancel()
	r, err := h.catalog.GetManga(ctx, &pb.GetMangaRequest{Id: c.Param("id")})
	if err != nil {
		writeMangaRPCError(c, err)
		return
	}
	c.JSON(200, mangaResponse(r.GetManga()))
}
func (h *mangaHandler) list(c *gin.Context) {
	if !h.available(c) {
		return
	}
	u := c.MustGet("identity").(auth.Identity)
	items, err := h.library.ListLibrary(c.Request.Context(), u.UserID)
	if err != nil {
		writeError(c, 500, "could not read library")
		return
	}
	c.JSON(200, gin.H{"library": items})
}
func (h *mangaHandler) add(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		MangaID string `json:"manga_id"`
	}
	if c.ShouldBindJSON(&input) != nil || input.MangaID == "" {
		writeError(c, 400, "manga_id is required")
		return
	}
	ctx, cancel := h.rpc(c)
	defer cancel()
	if _, err := h.catalog.GetManga(ctx, &pb.GetMangaRequest{Id: input.MangaID}); err != nil {
		writeMangaRPCError(c, err)
		return
	}
	u := c.MustGet("identity").(auth.Identity)
	entry, err := h.library.AddLibrary(c.Request.Context(), u.UserID, input.MangaID)
	if errors.Is(err, store.ErrLibraryExists) {
		writeError(c, 409, err.Error())
		return
	}
	if err != nil {
		writeError(c, 500, "could not add manga")
		return
	}
	c.JSON(201, entry)
}
func (h *mangaHandler) update(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		Chapter *int   `json:"current_chapter"`
		Status  string `json:"status"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Chapter == nil {
		writeError(c, 400, "integer current_chapter and status are required")
		return
	}
	if *input.Chapter < 0 || (input.Status != "plan-to-read" && input.Status != "reading" && input.Status != "completed") {
		writeError(c, 400, "invalid reading status or chapter")
		return
	}
	ctx, cancel := h.rpc(c)
	defer cancel()
	r, err := h.catalog.GetManga(ctx, &pb.GetMangaRequest{Id: c.Param("id")})
	if err != nil {
		writeMangaRPCError(c, err)
		return
	}
	if *input.Chapter > int(r.GetManga().GetTotalChapters()) {
		writeError(c, 400, "chapter exceeds total_chapters")
		return
	}
	u := c.MustGet("identity").(auth.Identity)
	// Serialize commit + enqueue so concurrent requests publish versions in order.
	h.progressMu.Lock()
	defer h.progressMu.Unlock()
	entry, err := h.library.UpdateLibrary(c.Request.Context(), u.UserID, c.Param("id"), input.Status, *input.Chapter)
	if errors.Is(err, store.ErrLibraryNotFound) {
		writeError(c, 404, "manga is not in your library")
		return
	}
	if err != nil {
		writeError(c, 500, "could not save reading progress")
		return
	}
	if h.events != nil {
		h.events.PublishMangaProgress(u.UserID, c.GetHeader("X-Session-ID"), entry)
	}
	c.JSON(200, entry)
}
func (h *mangaHandler) release(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		Total int32 `json:"total_chapters"`
	}
	if c.ShouldBindJSON(&input) != nil || input.Total < 1 {
		writeError(c, 400, "positive integer total_chapters is required")
		return
	}
	ctx, cancel := h.rpc(c)
	defer cancel()
	r, err := h.catalog.UpdateChapters(ctx, &pb.UpdateChaptersRequest{Id: c.Param("id"), TotalChapters: input.Total})
	if err != nil {
		writeMangaRPCError(c, err)
		return
	}
	m := mangaResponse(r.GetManga())
	if h.notifier != nil {
		h.notifier.NotifyNewChapter(m.ID, m.Title, m.TotalChapters)
	}
	c.JSON(http.StatusOK, m)
}
func writeMangaRPCError(c *gin.Context, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		writeError(c, 400, status.Convert(err).Message())
	case codes.NotFound:
		writeError(c, 404, "manga not found")
	case codes.DeadlineExceeded, codes.Unavailable:
		writeError(c, 503, "catalog unavailable or deadline exceeded")
	default:
		writeError(c, 502, "catalog request failed")
	}
}
