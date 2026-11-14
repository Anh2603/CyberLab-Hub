package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
)

func TestRegistrationLoginAndAuthorization(t *testing.T) {
	accounts, err := store.OpenBackend(context.Background(), filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatalf("OpenBackend() error = %v", err)
	}
	defer accounts.Close()
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	router := NewRouter(Dependencies{Accounts: accounts, Tokens: tokens})

	register := performJSON(router, http.MethodPost, "/api/auth/register", `{"username":"alice","password":"strong-password"}`, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", register.Code, register.Body.String())
	}
	var registration struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &registration); err != nil || registration.Token == "" {
		t.Fatalf("register token decode error = %v, body = %s", err, register.Body.String())
	}

	duplicate := performJSON(router, http.MethodPost, "/api/auth/register", `{"username":"ALICE","password":"strong-password"}`, "")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d", duplicate.Code, http.StatusConflict)
	}
	wrongPassword := performJSON(router, http.MethodPost, "/api/auth/login", `{"username":"alice","password":"wrong-password"}`, "")
	if wrongPassword.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password status = %d, want %d", wrongPassword.Code, http.StatusUnauthorized)
	}
	login := performJSON(router, http.MethodPost, "/api/auth/login", `{"username":"Alice","password":"strong-password"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}

	me := performJSON(router, http.MethodGet, "/api/me", "", registration.Token)
	if me.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", me.Code, me.Body.String())
	}
	adminOnly := performJSON(router, http.MethodGet, "/api/admin/health", "", registration.Token)
	if adminOnly.Code != http.StatusForbidden {
		t.Fatalf("learner admin status = %d, want %d", adminOnly.Code, http.StatusForbidden)
	}
	invalid := performJSON(router, http.MethodGet, "/api/me", "", "not-a-token")
	if invalid.Code != http.StatusUnauthorized {
		t.Fatalf("invalid-token status = %d, want %d", invalid.Code, http.StatusUnauthorized)
	}
}

func TestBootstrapAdminIsIdempotent(t *testing.T) {
	accounts, err := store.OpenBackend(context.Background(), filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatalf("OpenBackend() error = %v", err)
	}
	defer accounts.Close()
	created, err := BootstrapAdmin(context.Background(), accounts, "operator", "admin-password-123")
	if err != nil || !created {
		t.Fatalf("first BootstrapAdmin() created = %v, error = %v", created, err)
	}
	created, err = BootstrapAdmin(context.Background(), accounts, "operator", "admin-password-123")
	if err != nil || created {
		t.Fatalf("second BootstrapAdmin() created = %v, error = %v", created, err)
	}
	user, err := accounts.FindUserByUsername(context.Background(), "operator")
	if err != nil || user.Role != "admin" {
		t.Fatalf("bootstrap user = %#v, error = %v", user, err)
	}
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	token, err := tokens.Issue(auth.Identity{UserID: user.ID, Username: user.Username, Role: user.Role})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	adminOnly := performJSON(NewRouter(Dependencies{Accounts: accounts, Tokens: tokens}), http.MethodGet, "/api/admin/health", "", token)
	if adminOnly.Code != http.StatusOK {
		t.Fatalf("admin health status = %d, body = %s", adminOnly.Code, adminOnly.Body.String())
	}
}

func performJSON(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
