package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPagesAndAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router)

	tests := []struct {
		path        string
		contentType string
		contains    string
	}{
		{path: "/login", contentType: "text/html", contains: "Log in to CyberLab Hub"},
		{path: "/signup", contentType: "text/html", contains: "Create your account"},
		{path: "/app", contentType: "text/html", contains: "Connecting to CyberLab Hub"},
		{path: "/assets/app.css", contentType: "text/css", contains: "--primary: #4589ff"},
		{path: "/assets/auth.js", contentType: "text/javascript", contains: "/api/auth/login"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("content type = %q, want prefix %q", response.Header().Get("Content-Type"), test.contentType)
			}
			if !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("response does not contain %q", test.contains)
			}
			if response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("Content-Security-Policy header is missing")
			}
		})
	}
}

func TestRootRedirectsToLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	Register(router)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/login" {
		t.Fatalf("status/location = %d/%q, want %d/%q", response.Code, response.Header().Get("Location"), http.StatusFound, "/login")
	}
}
