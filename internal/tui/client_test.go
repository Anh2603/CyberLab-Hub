package tui

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Anh2603/MangaHub/internal/auth"
	"github.com/Anh2603/MangaHub/internal/store"
	tcpsync "github.com/Anh2603/MangaHub/internal/tcp"
	udpnotify "github.com/Anh2603/MangaHub/internal/udp"
	"github.com/gorilla/websocket"
)

func TestRoomClientSendsReceivesAndCancels(t *testing.T) {
	sentAt := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws/rooms/general" || r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		upgrader := websocket.Upgrader{}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		var input struct {
			Body string `json:"body"`
		}
		if err := connection.ReadJSON(&input); err != nil {
			return
		}
		_ = connection.WriteJSON(map[string]any{"type": "message", "username": "alice", "body": input.Body, "sent_at": sentAt})
		_, _, _ = connection.ReadMessage()
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "127.0.0.1:1", "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	client.token = "test-token"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 4)
	send, err := client.JoinRoom(ctx, "general", events)
	if err != nil {
		t.Fatal(err)
	}
	if err := send("hello"); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, sentAt.Local().Format("02/01/2006 15:04:05")+" alice: hello")
	cancel()
	select {
	case _, open := <-events:
		if open {
			t.Fatal("expected room events to close after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("room reader did not exit after cancellation")
	}
}

func TestHTTPClientWorkflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/auth/login" {
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"token": "token-123", "user": map[string]any{"id": 7, "username": "alice", "role": "admin"},
			})
			return
		}
		if request.Header.Get("Authorization") != "Bearer token-123" || !strings.HasPrefix(request.Header.Get("X-Session-ID"), "session-") {
			http.Error(writer, `{"error":"missing client credentials"}`, http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/api/manga":
			if request.URL.Query().Get("q") != "naruto" {
				t.Error("missing search query")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"manga": []store.Manga{{ID: "naruto", Title: "Naruto"}}})
		case "/api/library":
			if request.Method == http.MethodPost {
				writer.WriteHeader(http.StatusCreated)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"library": []store.LibraryEntry{{MangaID: "naruto", CurrentChapter: 5}}})
		case "/api/library/naruto/progress":
			var body struct {
				Chapter int    `json:"current_chapter"`
				Status  string `json:"status"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.Chapter != 5 || body.Status != "reading" {
				t.Error("incorrect progress payload")
			}
			_ = json.NewEncoder(writer).Encode(store.LibraryEntry{MangaID: "naruto", CurrentChapter: 5, Version: 2})
		case "/api/admin/manga/naruto/release":
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "127.0.0.1:1", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	user, err := client.Authenticate(context.Background(), "alice", "password", false)
	if err != nil || user.Role != "admin" {
		t.Fatalf("Authenticate() user = %#v, error = %v", user, err)
	}
	manga, err := client.SearchManga(context.Background(), "naruto")
	if err != nil || len(manga) != 1 || manga[0].ID != "naruto" {
		t.Fatalf("SearchManga = %v, %v", manga, err)
	}
	if err := client.AddManga(context.Background(), "naruto"); err != nil {
		t.Fatal(err)
	}
	progress, err := client.UpdateReading(context.Background(), "naruto", "reading", 5)
	if err != nil || progress.CurrentChapter != 5 || progress.Version != 2 {
		t.Fatalf("UpdateReading = %v, %v", progress, err)
	}
	library, err := client.Library(context.Background())
	if err != nil || len(library) != 1 {
		t.Fatalf("Library = %v, %v", library, err)
	}
	if err := client.ReleaseChapter(context.Background(), "naruto", 701); err != nil {
		t.Fatal(err)
	}
}

func TestRealtimeClientsReceiveTCPAndUDPEvents(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	token, err := manager.Issue(auth.Identity{UserID: 9, Username: "alice", Role: "learner"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("TCP Listen() error = %v", err)
	}
	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		tcpListener.Close()
		t.Fatalf("UDP Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tcpServer := tcpsync.NewServer(manager)
	udpServer := udpnotify.NewServer(manager)
	tcpDone, udpDone := make(chan error, 1), make(chan error, 1)
	go func() { tcpDone <- tcpServer.Serve(ctx, tcpListener) }()
	go func() { udpDone <- udpServer.Serve(ctx, udpListener) }()
	client, err := NewClient("http://127.0.0.1:1", tcpListener.Addr().String(), udpListener.LocalAddr().String())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.token = token
	client.user = User{ID: 9, Username: "alice", Role: "learner"}
	events, statuses := make(chan string, 16), make(chan string, 16)
	go client.RunProgressSync(ctx, events, statuses)
	go client.RunNotifications(ctx, events, statuses)
	waitForStatus(t, statuses, "TCP progress sync connected")
	tcpServer.PublishMangaProgress(9, "different-session", store.LibraryEntry{MangaID: "naruto", CurrentChapter: 5, Version: 2})
	waitForEvent(t, events, "Reading progress: naruto chapter 5 (v2)")

	deadline := time.Now().Add(2 * time.Second)
	for {
		udpServer.NotifyNewChapter("naruto", "Naruto", 701)
		select {
		case event := <-events:
			if event == "New chapter: Naruto — chapter 701" {
				cancel()
				<-tcpDone
				<-udpDone
				return
			}
		case <-time.After(25 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("UDP notification was not received")
		}
	}
}

func TestProgressSyncCancelsWhileAwaitingAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			accepted <- connection
		}
	}()
	client, err := NewClient("http://127.0.0.1:1", listener.Addr().String(), "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { client.RunProgressSync(ctx, make(chan string, 1), make(chan string, 8)); close(done) }()
	select {
	case connection := <-accepted:
		defer connection.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("TCP connection did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not unblock the pending authentication read")
	}
}

func waitForStatus(t *testing.T, statuses <-chan string, expected string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case status := <-statuses:
			if status == expected {
				return
			}
		case <-deadline:
			t.Fatalf("status %q was not received", expected)
		}
	}
}

func waitForEvent(t *testing.T, events <-chan string, expected string) {
	t.Helper()
	select {
	case event := <-events:
		if event != expected {
			t.Fatalf("event = %q, want %q", event, expected)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("event %q was not received", expected)
	}
}
