package tcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/Anh2603/MangaHub/internal/auth"
	"github.com/Anh2603/MangaHub/internal/store"
)

func TestProgressDeliveryIsPrivateAndExcludesOrigin(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	server := NewServer(manager)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	defer func() {
		cancel()
		<-done
	}()

	userToken := issueToken(t, manager, 11, "alice")
	otherToken := issueToken(t, manager, 22, "bob")
	origin, originReader := connectSession(t, listener.Addr().String(), userToken, "session-1111")
	defer origin.Close()
	receiver, receiverReader := connectSession(t, listener.Addr().String(), userToken, "session-2222")
	defer receiver.Close()
	other, otherReader := connectSession(t, listener.Addr().String(), otherToken, "session-3333")
	defer other.Close()

	server.PublishMangaProgress(11, "session-1111", store.LibraryEntry{MangaID: "naruto", CurrentChapter: 5, Version: 2})
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	var event struct {
		Type    string `json:"type"`
		MangaID string `json:"manga_id"`
		Chapter int    `json:"chapter"`
		Version int64  `json:"version"`
	}
	if err := readLineJSON(receiverReader, &event); err != nil {
		t.Fatalf("receiver event error = %v", err)
	}
	if event.Type != "progress_updated" || event.MangaID != "naruto" || event.Chapter != 5 || event.Version != 2 {
		t.Fatalf("receiver event = %#v", event)
	}
	assertNoLine(t, origin, originReader, "origin")
	assertNoLine(t, other, otherReader, "other user")
	_ = receiver.Close()
	deadline := time.Now().Add(time.Second)
	for {
		server.hub.mu.Lock()
		_, connected := server.hub.sessions[11]["session-2222"]
		server.hub.mu.Unlock()
		if !connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected session was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestInvalidTokenIsRejected(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	server := NewServer(manager)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	connection, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	_, _ = connection.Write([]byte(`{"type":"authenticate","token":"invalid","session_id":"session-4444"}` + "\n"))
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	var response map[string]string
	if err := readLineJSON(bufio.NewReader(connection), &response); err != nil {
		t.Fatalf("read rejection error = %v", err)
	}
	if response["type"] != "error" || response["error"] != "invalid token" {
		t.Fatalf("rejection response = %#v", response)
	}
	connection.Close()
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func issueToken(t *testing.T, manager *auth.Manager, userID int64, username string) string {
	t.Helper()
	token, err := manager.Issue(auth.Identity{UserID: userID, Username: username, Role: "learner"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return token
}

func connectSession(t *testing.T, address, token, sessionID string) (net.Conn, *bufio.Reader) {
	t.Helper()
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	payload, _ := json.Marshal(map[string]string{
		"type": "authenticate", "token": token, "session_id": sessionID,
	})
	if _, err := connection.Write(append(payload, '\n')); err != nil {
		connection.Close()
		t.Fatalf("authenticate write error = %v", err)
	}
	reader := bufio.NewReader(connection)
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	var response map[string]string
	if err := readLineJSON(reader, &response); err != nil || response["type"] != "authenticated" {
		connection.Close()
		t.Fatalf("authenticate response = %#v, error = %v", response, err)
	}
	return connection, reader
}

func readLineJSON(reader *bufio.Reader, destination any) error {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, destination)
}

func assertNoLine(t *testing.T, connection net.Conn, reader *bufio.Reader, label string) {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	var message any
	err := readLineJSON(reader, &message)
	if err == nil {
		t.Fatalf("%s unexpectedly received %#v", label, message)
	}
	if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("%s read error = %v, want timeout", label, err)
	}
}
