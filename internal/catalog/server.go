package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ServiceName = "catalog.v1.CatalogService"

var challengeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)

type Server struct {
	catalogv1.UnimplementedCatalogServiceServer
	store *store.Catalog
}

func NewServer(catalogStore *store.Catalog) *Server {
	return &Server{store: catalogStore}
}

func (s *Server) SearchChallenges(ctx context.Context, request *catalogv1.SearchChallengesRequest) (*catalogv1.SearchChallengesResponse, error) {
	difficulty := strings.ToLower(strings.TrimSpace(request.GetDifficulty()))
	if difficulty != "" && !validDifficulty(difficulty) {
		return nil, status.Error(codes.InvalidArgument, "difficulty must be easy, medium, or hard")
	}
	challenges, err := s.store.SearchChallenges(ctx, strings.TrimSpace(request.GetQuery()),
		strings.ToLower(strings.TrimSpace(request.GetCategory())), difficulty)
	if err != nil {
		return nil, status.Error(codes.Internal, "challenge search failed")
	}
	response := &catalogv1.SearchChallengesResponse{Challenges: make([]*catalogv1.Challenge, 0, len(challenges))}
	for _, challenge := range challenges {
		response.Challenges = append(response.Challenges, publicChallenge(challenge))
	}
	return response, nil
}

func (s *Server) GetChallenge(ctx context.Context, request *catalogv1.GetChallengeRequest) (*catalogv1.GetChallengeResponse, error) {
	id := strings.TrimSpace(request.GetId())
	if !challengeIDPattern.MatchString(id) {
		return nil, status.Error(codes.InvalidArgument, "invalid challenge id")
	}
	challenge, err := s.store.GetChallenge(ctx, id, request.GetIncludeUnpublished())
	if errors.Is(err, store.ErrChallengeNotFound) {
		return nil, status.Error(codes.NotFound, "challenge not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "challenge lookup failed")
	}
	return &catalogv1.GetChallengeResponse{Challenge: publicChallenge(challenge)}, nil
}

func (s *Server) VerifyAnswer(ctx context.Context, request *catalogv1.VerifyAnswerRequest) (*catalogv1.VerifyAnswerResponse, error) {
	id := strings.TrimSpace(request.GetChallengeId())
	answer := normalizeAnswer(request.GetAnswer())
	if !challengeIDPattern.MatchString(id) || answer == "" {
		return nil, status.Error(codes.InvalidArgument, "challenge id and answer are required")
	}
	challenge, err := s.store.GetChallenge(ctx, id, false)
	if errors.Is(err, store.ErrChallengeNotFound) {
		return nil, status.Error(codes.NotFound, "challenge not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "answer verification failed")
	}
	correct := bcrypt.CompareHashAndPassword(challenge.AnswerHash, []byte(answer)) == nil
	return &catalogv1.VerifyAnswerResponse{Correct: correct}, nil
}

func (s *Server) PublishChallenge(ctx context.Context, request *catalogv1.PublishChallengeRequest) (*catalogv1.PublishChallengeResponse, error) {
	challenge, answer, err := validatePublication(request)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(answer), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "answer must not exceed 72 bytes")
	}
	challenge.AnswerHash = hash
	published, err := s.store.PublishChallenge(ctx, challenge)
	if err != nil {
		return nil, status.Error(codes.Internal, "challenge publication failed")
	}
	return &catalogv1.PublishChallengeResponse{Challenge: publicChallenge(published)}, nil
}

func (s *Server) ArchiveChallenge(ctx context.Context, request *catalogv1.ArchiveChallengeRequest) (*catalogv1.ArchiveChallengeResponse, error) {
	id := strings.TrimSpace(request.GetId())
	if !challengeIDPattern.MatchString(id) {
		return nil, status.Error(codes.InvalidArgument, "invalid challenge id")
	}
	archived, err := s.store.ArchiveChallenge(ctx, id)
	if errors.Is(err, store.ErrChallengeNotFound) {
		return nil, status.Error(codes.NotFound, "challenge not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "challenge archive failed")
	}
	return &catalogv1.ArchiveChallengeResponse{Challenge: publicChallenge(archived)}, nil
}

func SeedDefaults(ctx context.Context, catalogStore *store.Catalog) error {
	count, err := catalogStore.ChallengeCount(ctx)
	if err != nil {
		return err
	}
	seeds := []struct {
		challenge          store.Challenge
		answer             string
		previousDifficulty string
	}{
		{store.Challenge{ID: "dns-log-basics", Title: "DNS Log Basics", Description: "Synthetic DNS log:\n2026-11-18T08:00:00Z client=192.0.2.10 query=dns-basics.example type=A\n\nFind the label before .example. Replace dashes with underscores and submit flag{label}.", Category: "log-analysis", Difficulty: "easy"}, "flag{dns_basics}", ""},
		{store.Challenge{ID: "http-header-review", Title: "HTTP Header Review", Description: "Synthetic HTTP response:\nHTTP/1.1 200 OK\nContent-Type: text/html\nX-Content-Type-Options: nosniff\n\nWhich missing response header restricts the sources a browser may load scripts from? Submit its lowercase name with underscores instead of dashes: flag{header_name}.", Category: "web-security", Difficulty: "medium"}, "flag{content_security_policy}", "easy"},
		{store.Challenge{ID: "file-signature", Title: "File Signature", Description: "A synthetic file begins with these hexadecimal bytes:\n89 50 4E 47 0D 0A 1A 0A\n\nIdentify the file format and submit flag{lowercase_extension}.", Category: "forensics", Difficulty: "easy"}, "flag{png}", "medium"},
	}
	legacyDescriptions := map[string]string{
		"dns-log-basics":     "Identify the queried domain in a synthetic DNS log.",
		"http-header-review": "Inspect response headers and identify the missing protection.",
		"file-signature":     "Recognize a file type from its hexadecimal magic bytes.",
	}
	for _, seed := range seeds {
		if count != 0 {
			if err := catalogStore.UpdateLegacyDescription(ctx, seed.challenge.ID, legacyDescriptions[seed.challenge.ID], seed.challenge.Description); err != nil {
				return err
			}
			if seed.previousDifficulty != "" {
				if err := catalogStore.UpdateSeedDifficulty(ctx, seed.challenge.ID, seed.challenge.Title, seed.challenge.Description, seed.challenge.Category, seed.previousDifficulty, seed.challenge.Difficulty); err != nil {
					return err
				}
			}
			continue
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(seed.answer), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		seed.challenge.AnswerHash = hash
		if _, err := catalogStore.PublishChallenge(ctx, seed.challenge); err != nil {
			return err
		}
	}
	return nil
}

func validatePublication(request *catalogv1.PublishChallengeRequest) (store.Challenge, string, error) {
	challenge := store.Challenge{
		ID:          strings.TrimSpace(request.GetId()),
		Title:       strings.TrimSpace(request.GetTitle()),
		Description: strings.TrimSpace(request.GetDescription()),
		Category:    strings.ToLower(strings.TrimSpace(request.GetCategory())),
		Difficulty:  strings.ToLower(strings.TrimSpace(request.GetDifficulty())),
	}
	answer := normalizeAnswer(request.GetAnswer())
	if !challengeIDPattern.MatchString(challenge.ID) {
		return store.Challenge{}, "", errors.New("id must contain 3-64 lowercase letters, numbers, or dashes")
	}
	if challenge.Title == "" || len(challenge.Title) > 120 {
		return store.Challenge{}, "", errors.New("title must contain 1-120 characters")
	}
	if challenge.Description == "" || len(challenge.Description) > 4000 {
		return store.Challenge{}, "", errors.New("description must contain 1-4000 characters")
	}
	if challenge.Category == "" || len(challenge.Category) > 64 {
		return store.Challenge{}, "", errors.New("category must contain 1-64 characters")
	}
	if !validDifficulty(challenge.Difficulty) {
		return store.Challenge{}, "", errors.New("difficulty must be easy, medium, or hard")
	}
	if answer == "" || len(answer) > 72 {
		return store.Challenge{}, "", errors.New("answer must contain 1-72 bytes")
	}
	return challenge, answer, nil
}

func validDifficulty(value string) bool {
	return value == "easy" || value == "medium" || value == "hard"
}

func normalizeAnswer(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func publicChallenge(challenge store.Challenge) *catalogv1.Challenge {
	return &catalogv1.Challenge{
		Id:          challenge.ID,
		Title:       challenge.Title,
		Description: challenge.Description,
		Category:    challenge.Category,
		Difficulty:  challenge.Difficulty,
		Published:   challenge.Published,
	}
}
