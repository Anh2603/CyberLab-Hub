package catalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	pb "github.com/Anh2603/MangaHub/gen/catalog/v1"
	"github.com/Anh2603/MangaHub/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const MangaServiceName = "catalog.v1.MangaCatalogService"

var mangaIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type MangaServer struct {
	pb.UnimplementedMangaCatalogServiceServer
	catalog *store.Catalog
}

func NewMangaServer(catalog *store.Catalog) *MangaServer { return &MangaServer{catalog: catalog} }
func mangaProto(m store.Manga) *pb.Manga {
	return &pb.Manga{Id: m.ID, Title: m.Title, Author: m.Author, Genres: m.Genres, Status: m.Status, TotalChapters: int32(m.TotalChapters), Description: m.Description}
}
func (s *MangaServer) SearchManga(ctx context.Context, r *pb.SearchMangaRequest) (*pb.SearchMangaResponse, error) {
	if len(r.GetQuery()) > 200 {
		return nil, status.Error(codes.InvalidArgument, "query is too long")
	}
	items, err := s.catalog.SearchManga(ctx, r.GetQuery())
	if err != nil {
		return nil, status.Error(codes.Internal, "catalog search failed")
	}
	result := &pb.SearchMangaResponse{Manga: []*pb.Manga{}}
	for _, m := range items {
		result.Manga = append(result.Manga, mangaProto(m))
	}
	return result, nil
}
func (s *MangaServer) GetManga(ctx context.Context, r *pb.GetMangaRequest) (*pb.GetMangaResponse, error) {
	if !mangaIDPattern.MatchString(r.GetId()) {
		return nil, status.Error(codes.InvalidArgument, "invalid manga id")
	}
	m, err := s.catalog.GetManga(ctx, r.GetId())
	if err != nil {
		return nil, mangaError(err)
	}
	return &pb.GetMangaResponse{Manga: mangaProto(m)}, nil
}
func (s *MangaServer) UpdateChapters(ctx context.Context, r *pb.UpdateChaptersRequest) (*pb.UpdateChaptersResponse, error) {
	if !mangaIDPattern.MatchString(r.GetId()) || r.GetTotalChapters() < 1 || r.GetTotalChapters() > 1000000 {
		return nil, status.Error(codes.InvalidArgument, "invalid manga id or chapter count")
	}
	m, err := s.catalog.UpdateChapters(ctx, r.GetId(), int(r.GetTotalChapters()))
	if err != nil {
		return nil, mangaError(err)
	}
	return &pb.UpdateChaptersResponse{Manga: mangaProto(m)}, nil
}
func mangaError(err error) error {
	if errors.Is(err, store.ErrMangaNotFound) {
		return status.Error(codes.NotFound, "manga not found")
	}
	if errors.Is(err, store.ErrInvalidChapters) {
		return status.Error(codes.InvalidArgument, "new chapter count must exceed the current total")
	}
	return status.Error(codes.Internal, "catalog operation failed")
}

//go:embed manga_seed.json
var mangaSeedJSON []byte

func RealMangaSeeds() ([]store.Manga, error) {
	var seeds []store.Manga
	if err := json.Unmarshal(mangaSeedJSON, &seeds); err != nil {
		return nil, err
	}
	return seeds, nil
}

func SeedManga(ctx context.Context, catalog *store.Catalog) error {
	seeds, err := RealMangaSeeds()
	if err != nil {
		return err
	}
	return catalog.SeedManga(ctx, seeds)
}

// Refresh is an explicit operator action. Normal startup still seeds only an
// empty catalog. Old fictional entries remain accessible to existing libraries.
func RefreshMangaCatalog(ctx context.Context, catalog *store.Catalog) error {
	seeds, err := RealMangaSeeds()
	if err != nil {
		return err
	}
	return catalog.InstallRealManga(ctx, seeds, legacyMangaSeeds())
}

func legacyMangaSeeds() []store.Manga {
	titles := []string{"Star Harbor", "Clockwork Garden", "Moonlit Courier", "Paper Dragon", "Skyline Runners", "Ocean of Lanterns", "Silver Orchard", "Midnight Academy", "Forest of Echoes", "Crimson Compass", "Cloud Kitchen", "Winter Sonata Club", "Solar Knights", "Quiet Library", "Thunder Post", "Sapphire Trails", "Amber Detectives", "Dream Atlas", "Neon Workshop", "River Guardians", "Lost Constellation", "Tea House Days", "Iron Petals", "Summer Sketchbook", "Polar Expedition", "Secret Observatory", "Lantern Festival", "Wandering Alchemist", "Velvet Court", "Tomorrow Express"}
	genres := []string{"Adventure", "Fantasy", "Science Fiction", "Mystery", "Slice of Life", "Sports"}
	creators := []string{"M. Aoki", "R. Mori", "S. Hoshino", "K. Sato", "Y. Kato"}
	seeds := make([]store.Manga, 0, len(titles))
	for i, title := range titles {
		state := "ongoing"
		if i%5 == 0 {
			state = "completed"
		}
		if i%11 == 0 {
			state = "hiatus"
		}
		seeds = append(seeds, store.Manga{ID: strings.ReplaceAll(strings.ToLower(title), " ", "-"), Title: title, Author: creators[i%len(creators)], Genres: []string{genres[i%len(genres)], genres[(i+1)%len(genres)]}, Status: state, TotalChapters: 12 + i*7, Description: fmt.Sprintf("An original classroom manga metadata sample about %s. Follow its characters through a %s story. Metadata only; no reading pages are hosted.", strings.ToLower(title), strings.ToLower(genres[i%len(genres)]))})
	}
	return seeds
}
