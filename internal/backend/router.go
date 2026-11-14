package backend

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"github.com/gin-gonic/gin"
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,32}$`)

type AccountStore interface {
	CreateUser(ctx context.Context, username string, passwordHash []byte, role string) (store.User, error)
	FindUserByUsername(ctx context.Context, username string) (store.User, error)
}

type Dependencies struct {
	Accounts   AccountStore
	Tokens     *auth.Manager
	Catalog    CatalogClient
	Progress   ProgressStore
	RPCTimeout time.Duration
}

func NewRouter(deps Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), limitRequestBody(64<<10))
	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "cyberlab-backend",
			"status":  "ok",
		})
	})

	handler := accountHandler{accounts: deps.Accounts, tokens: deps.Tokens}
	router.POST("/api/auth/register", handler.register)
	router.POST("/api/auth/login", handler.login)
	authenticated := router.Group("/api", handler.authenticate)
	authenticated.GET("/me", handler.me)
	admin := authenticated.Group("/admin", handler.requireAdmin)
	admin.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "scope": "admin"})
	})
	challengeHandler{catalog: deps.Catalog, progress: deps.Progress, timeout: deps.RPCTimeout}.register(authenticated, admin)
	return router
}

func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

type accountHandler struct {
	accounts AccountStore
	tokens   *auth.Manager
}

type credentials struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h accountHandler) register(c *gin.Context) {
	if h.accounts == nil || h.tokens == nil {
		writeError(c, http.StatusServiceUnavailable, "authentication service unavailable")
		return
	}
	var input credentials
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "username and password are required")
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !usernamePattern.MatchString(input.Username) {
		writeError(c, http.StatusBadRequest, "username must be 3-32 letters, numbers, dots, dashes, or underscores")
		return
	}
	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	user, err := h.accounts.CreateUser(c.Request.Context(), input.Username, hash, "learner")
	if errors.Is(err, store.ErrUserExists) {
		writeError(c, http.StatusConflict, "username already exists")
		return
	}
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create account")
		return
	}
	h.respondWithToken(c, http.StatusCreated, user)
}

func (h accountHandler) login(c *gin.Context) {
	if h.accounts == nil || h.tokens == nil {
		writeError(c, http.StatusServiceUnavailable, "authentication service unavailable")
		return
	}
	var input credentials
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, http.StatusBadRequest, "username and password are required")
		return
	}
	user, err := h.accounts.FindUserByUsername(c.Request.Context(), strings.TrimSpace(input.Username))
	if err != nil || !auth.VerifyPassword(user.PasswordHash, input.Password) {
		writeError(c, http.StatusUnauthorized, "invalid username or password")
		return
	}
	h.respondWithToken(c, http.StatusOK, user)
}

func (h accountHandler) respondWithToken(c *gin.Context, status int, user store.User) {
	token, err := h.tokens.Issue(auth.Identity{UserID: user.ID, Username: user.Username, Role: user.Role})
	if err != nil {
		writeError(c, http.StatusInternalServerError, "could not create session")
		return
	}
	c.JSON(status, gin.H{
		"token": token,
		"user":  gin.H{"id": user.ID, "username": user.Username, "role": user.Role},
	})
}

func (h accountHandler) authenticate(c *gin.Context) {
	if h.tokens == nil {
		writeError(c, http.StatusServiceUnavailable, "authentication service unavailable")
		c.Abort()
		return
	}
	header := c.GetHeader("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		writeError(c, http.StatusUnauthorized, "bearer token required")
		c.Abort()
		return
	}
	identity, err := h.tokens.Parse(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	if err != nil {
		writeError(c, http.StatusUnauthorized, "invalid or expired token")
		c.Abort()
		return
	}
	c.Set("identity", identity)
	c.Next()
}

func (h accountHandler) requireAdmin(c *gin.Context) {
	identity, ok := c.MustGet("identity").(auth.Identity)
	if !ok || identity.Role != "admin" {
		writeError(c, http.StatusForbidden, "administrator role required")
		c.Abort()
		return
	}
	c.Next()
}

func (h accountHandler) me(c *gin.Context) {
	identity := c.MustGet("identity").(auth.Identity)
	c.JSON(http.StatusOK, gin.H{"id": identity.UserID, "username": identity.Username, "role": identity.Role})
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}
