package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Anh2603/MangaHub/internal/auth"
	"github.com/Anh2603/MangaHub/internal/backend"
	"github.com/Anh2603/MangaHub/internal/store"
	"github.com/gorilla/websocket"
)

func TestAuthenticatedRoomDeliveryAndIsolation(t *testing.T) {
	hub, server, tokens, accounts := testDiscussionServer(t)
	defer server.Close()
	defer accounts.Close()
	aliceToken := createUserToken(t, accounts, tokens, "alice")
	bobToken := createUserToken(t, accounts, tokens, "bob")

	aliceOne := dialRoom(t, server.URL, "log-analysis", aliceToken)
	defer aliceOne.Close()
	aliceTwo := dialRoom(t, server.URL, "log-analysis", aliceToken)
	defer aliceTwo.Close()
	bob := dialRoom(t, server.URL, "forensics", bobToken)
	defer bob.Close()

	if err := aliceOne.WriteJSON(map[string]string{"body": "Check the DNS query"}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	_ = aliceTwo.SetReadDeadline(time.Now().Add(time.Second))
	var message map[string]any
	if err := aliceTwo.ReadJSON(&message); err != nil {
		t.Fatalf("same-room ReadJSON() error = %v", err)
	}
	if message["room"] != "log-analysis" || message["username"] != "alice" || message["body"] != "Check the DNS query" {
		t.Fatalf("same-room message = %#v", message)
	}
	_ = bob.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	if err := bob.ReadJSON(&message); err == nil {
		t.Fatalf("other room unexpectedly received %#v", message)
	}

	_ = aliceTwo.Close()
	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.Lock()
		count := len(hub.rooms["log-analysis"])
		hub.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("room participant count did not fall to 1")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWebSocketRejectsInvalidTokenAndRoom(t *testing.T) {
	_, server, tokens, accounts := testDiscussionServer(t)
	defer server.Close()
	defer accounts.Close()
	_, response, err := websocket.DefaultDialer.Dial(websocketURL(server.URL, "log-analysis"),
		http.Header{"Authorization": []string{"Bearer invalid"}})
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid-token handshake error = %v, response = %#v", err, response)
	}
	if response.Body != nil {
		response.Body.Close()
	}
	validToken := createUserToken(t, accounts, tokens, "charlie")
	_, response, err = websocket.DefaultDialer.Dial(websocketURL(server.URL, "INVALID ROOM"),
		http.Header{"Authorization": []string{"Bearer " + validToken}})
	if err == nil || response == nil || response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid-room handshake error = %v, response = %#v", err, response)
	}
	if response.Body != nil {
		response.Body.Close()
	}
}

func testDiscussionServer(t *testing.T) (*Hub, *httptest.Server, *auth.Manager, *store.Backend) {
	t.Helper()
	accounts, err := store.OpenBackend(context.Background(), filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatalf("OpenBackend() error = %v", err)
	}
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	hub := NewHub()
	router := backend.NewRouter(backend.Dependencies{Accounts: accounts, Tokens: tokens, Discussions: hub})
	return hub, httptest.NewServer(router), tokens, accounts
}

func createUserToken(t *testing.T, accounts *store.Backend, tokens *auth.Manager, username string) string {
	t.Helper()
	hash, err := auth.HashPassword("strong-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	user, err := accounts.CreateUser(context.Background(), username, hash, "learner")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	token, err := tokens.Issue(auth.Identity{UserID: user.ID, Username: user.Username, Role: user.Role})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return token
}

func dialRoom(t *testing.T, serverURL, room, token string) *websocket.Conn {
	t.Helper()
	connection, response, err := websocket.DefaultDialer.Dial(websocketURL(serverURL, room),
		http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		t.Fatalf("Dial() room %s error = %v", room, err)
	}
	return connection
}

func websocketURL(serverURL, room string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http") + "/api/ws/rooms/" + room
}
