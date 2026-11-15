package backend

import (
	"context"
	"net/http"
	"strings"
	"time"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CatalogClient interface {
	SearchChallenges(context.Context, *catalogv1.SearchChallengesRequest, ...grpc.CallOption) (*catalogv1.SearchChallengesResponse, error)
	GetChallenge(context.Context, *catalogv1.GetChallengeRequest, ...grpc.CallOption) (*catalogv1.GetChallengeResponse, error)
	VerifyAnswer(context.Context, *catalogv1.VerifyAnswerRequest, ...grpc.CallOption) (*catalogv1.VerifyAnswerResponse, error)
	PublishChallenge(context.Context, *catalogv1.PublishChallengeRequest, ...grpc.CallOption) (*catalogv1.PublishChallengeResponse, error)
	ArchiveChallenge(context.Context, *catalogv1.ArchiveChallengeRequest, ...grpc.CallOption) (*catalogv1.ArchiveChallengeResponse, error)
}

type ProgressStore interface {
	RecordSubmission(context.Context, int64, string, bool) error
	ListProgress(context.Context, int64) ([]store.Progress, error)
}

type ProgressPublisher interface {
	PublishProgress(userID int64, originSessionID, challengeID string, solvedAt time.Time)
}

type PublicationNotifier interface {
	NotifyChallengePublished(id, title, category, difficulty string)
}

type challengeHandler struct {
	catalog  CatalogClient
	progress ProgressStore
	events   ProgressPublisher
	notifier PublicationNotifier
	timeout  time.Duration
}

type challengeJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Published   bool   `json:"published"`
}

func (h challengeHandler) register(authenticated, admin *gin.RouterGroup) {
	authenticated.GET("/challenges", h.search)
	authenticated.GET("/challenges/:id", h.get)
	authenticated.POST("/challenges/:id/submissions", h.submit)
	authenticated.GET("/progress", h.listProgress)
	admin.POST("/challenges", h.publish)
	admin.DELETE("/challenges/:id", h.archive)
}

func (h challengeHandler) search(c *gin.Context) {
	if !h.available(c) {
		return
	}
	ctx, cancel := h.rpcContext(c)
	defer cancel()
	response, err := h.catalog.SearchChallenges(ctx, &catalogv1.SearchChallengesRequest{
		Query: c.Query("query"), Category: c.Query("category"), Difficulty: c.Query("difficulty"),
	})
	if err != nil {
		writeRPCError(c, err)
		return
	}
	challenges := make([]challengeJSON, 0, len(response.GetChallenges()))
	for _, challenge := range response.GetChallenges() {
		challenges = append(challenges, challengeResponse(challenge))
	}
	c.JSON(http.StatusOK, gin.H{"challenges": challenges})
}

func (h challengeHandler) get(c *gin.Context) {
	if !h.available(c) {
		return
	}
	ctx, cancel := h.rpcContext(c)
	defer cancel()
	response, err := h.catalog.GetChallenge(ctx, &catalogv1.GetChallengeRequest{Id: c.Param("id")})
	if err != nil {
		writeRPCError(c, err)
		return
	}
	c.JSON(http.StatusOK, challengeResponse(response.GetChallenge()))
}

func (h challengeHandler) submit(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		Answer string `json:"answer" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Answer) == "" {
		writeError(c, http.StatusBadRequest, "answer is required")
		return
	}
	ctx, cancel := h.rpcContext(c)
	defer cancel()
	result, err := h.catalog.VerifyAnswer(ctx, &catalogv1.VerifyAnswerRequest{
		ChallengeId: c.Param("id"), Answer: input.Answer,
	})
	if err != nil {
		writeRPCError(c, err)
		return
	}
	identity := c.MustGet("identity").(auth.Identity)
	if err := h.progress.RecordSubmission(c.Request.Context(), identity.UserID, c.Param("id"), result.GetCorrect()); err != nil {
		writeError(c, http.StatusInternalServerError, "could not record submission")
		return
	}
	if result.GetCorrect() && h.events != nil {
		h.events.PublishProgress(identity.UserID, c.GetHeader("X-Session-ID"), c.Param("id"), time.Now())
	}
	c.JSON(http.StatusOK, gin.H{"correct": result.GetCorrect()})
}

func (h challengeHandler) listProgress(c *gin.Context) {
	if h.progress == nil {
		writeError(c, http.StatusServiceUnavailable, "progress service unavailable")
		return
	}
	identity := c.MustGet("identity").(auth.Identity)
	items, err := h.progress.ListProgress(c.Request.Context(), identity.UserID)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not load progress")
		return
	}
	response := make([]gin.H, 0, len(items))
	for _, item := range items {
		response = append(response, gin.H{"challenge_id": item.ChallengeID, "solved_at": item.SolvedAt})
	}
	c.JSON(http.StatusOK, gin.H{"progress": response})
}

func (h challengeHandler) publish(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var input struct {
		ID          string `json:"id" binding:"required"`
		Title       string `json:"title" binding:"required"`
		Description string `json:"description" binding:"required"`
		Category    string `json:"category" binding:"required"`
		Difficulty  string `json:"difficulty" binding:"required"`
		Answer      string `json:"answer" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "all challenge fields are required")
		return
	}
	ctx, cancel := h.rpcContext(c)
	defer cancel()
	response, err := h.catalog.PublishChallenge(ctx, &catalogv1.PublishChallengeRequest{
		Id: input.ID, Title: input.Title, Description: input.Description,
		Category: input.Category, Difficulty: input.Difficulty, Answer: input.Answer,
	})
	if err != nil {
		writeRPCError(c, err)
		return
	}
	challenge := challengeResponse(response.GetChallenge())
	if h.notifier != nil {
		h.notifier.NotifyChallengePublished(challenge.ID, challenge.Title, challenge.Category, challenge.Difficulty)
	}
	c.JSON(http.StatusCreated, challenge)
}

func (h challengeHandler) archive(c *gin.Context) {
	if !h.available(c) {
		return
	}
	ctx, cancel := h.rpcContext(c)
	defer cancel()
	response, err := h.catalog.ArchiveChallenge(ctx, &catalogv1.ArchiveChallengeRequest{Id: c.Param("id")})
	if err != nil {
		writeRPCError(c, err)
		return
	}
	c.JSON(http.StatusOK, challengeResponse(response.GetChallenge()))
}

func (h challengeHandler) available(c *gin.Context) bool {
	if h.catalog == nil || h.progress == nil {
		writeError(c, http.StatusServiceUnavailable, "challenge service unavailable")
		return false
	}
	return true
}

func (h challengeHandler) rpcContext(c *gin.Context) (context.Context, context.CancelFunc) {
	timeout := h.timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return context.WithTimeout(c.Request.Context(), timeout)
}

func challengeResponse(challenge *catalogv1.Challenge) challengeJSON {
	return challengeJSON{
		ID: challenge.GetId(), Title: challenge.GetTitle(), Description: challenge.GetDescription(),
		Category: challenge.GetCategory(), Difficulty: challenge.GetDifficulty(), Published: challenge.GetPublished(),
	}
}

func writeRPCError(c *gin.Context, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		writeError(c, http.StatusBadRequest, status.Convert(err).Message())
	case codes.NotFound:
		writeError(c, http.StatusNotFound, "challenge not found")
	case codes.DeadlineExceeded:
		writeError(c, http.StatusGatewayTimeout, "challenge service timed out")
	case codes.Unavailable:
		writeError(c, http.StatusServiceUnavailable, "challenge service unavailable")
	default:
		writeError(c, http.StatusBadGateway, "challenge service request failed")
	}
}
