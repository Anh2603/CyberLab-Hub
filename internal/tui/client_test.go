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

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	tcpsync "github.com/Anh2603/CyberLab-Hub/internal/tcp"
	udpnotify "github.com/Anh2603/CyberLab-Hub/internal/udp"
	"github.com/gorilla/websocket"
)

func TestRoomClientSendsReceivesAndCancels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws/rooms/log-analysis" || r.Header.Get("Authorization") != "Bearer test-token" {
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
		_ = connection.WriteJSON(map[string]string{"username": "alice", "body": input.Body})
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
	send, err := client.JoinRoom(ctx, "log-analysis", events)
	if err != nil {
		t.Fatal(err)
	}
	if err := send("hello"); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "alice: hello")
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
		switch {
		case request.URL.Path == "/api/challenges":
			_ = json.NewEncoder(writer).Encode(map[string]any{"challenges": []map[string]any{{"id": "dns-log-basics", "title": "DNS", "published": true}}})
		case strings.HasSuffix(request.URL.Path, "/submissions"):
			_ = json.NewEncoder(writer).Encode(map[string]bool{"correct": true})
		case request.URL.Path == "/api/progress":
			_ = json.NewEncoder(writer).Encode(map[string]any{"progress": []map[string]any{{"challenge_id": "dns-log-basics", "solved_at": time.Now().UTC()}}})
		case request.URL.Path == "/api/admin/challenges":
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(map[string]string{"id": "new-task"})
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
	challenges, err := client.SearchChallenges(context.Background(), "dns")
	if err != nil || len(challenges) != 1 {
		t.Fatalf("SearchChallenges() count = %d, error = %v", len(challenges), err)
	}
	correct, err := client.SubmitAnswer(context.Background(), "dns-log-basics", "flag{dns_basics}")
	if err != nil || !correct {
		t.Fatalf("SubmitAnswer() correct = %v, error = %v", correct, err)
	}
	progress, err := client.Progress(context.Background())
	if err != nil || len(progress) != 1 {
		t.Fatalf("Progress() count = %d, error = %v", len(progress), err)
	}
	if err := client.PublishChallenge(context.Background(), Challenge{ID: "new-task"}, "flag{new}"); err != nil {
		t.Fatalf("PublishChallenge() error = %v", err)
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
	tcpServer.PublishProgress(9, "different-session", "dns-log-basics", time.Now())
	waitForEvent(t, events, "Progress synchronized: dns-log-basics")

	deadline := time.Now().Add(2 * time.Second)
	for {
		udpServer.NotifyChallengePublished("new-task", "New Task", "network", "easy")
		select {
		case event := <-events:
			if event == "New challenge: New Task" {
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
