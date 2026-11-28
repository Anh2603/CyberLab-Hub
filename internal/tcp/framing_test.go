package tcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
)

func TestTCPFramingRejectsUnterminatedEOF(t *testing.T) {
	scanner := newFrameScanner(strings.NewReader("{\"type\":\"ping\"}\n{\"type\":\"ping\"}"))
	if !scanner.Scan() || string(scanner.Bytes()) != `{"type":"ping"}` {
		t.Fatal("complete frame was not accepted")
	}
	if scanner.Scan() {
		t.Fatalf("unterminated frame accepted: %s", scanner.Bytes())
	}
	if scanner.Err() == nil {
		t.Fatal("unterminated EOF was not reported")
	}
}

func TestTCPFramingScannerSizeBoundary(t *testing.T) {
	for _, size := range []int{maxMessageBytes, maxMessageBytes + 1} {
		scanner := newFrameScanner(strings.NewReader(strings.Repeat("x", size) + "\n"))
		if size == maxMessageBytes {
			if !scanner.Scan() || len(scanner.Bytes()) != size {
				t.Fatalf("exact size boundary rejected: %v", scanner.Err())
			}
			if scanner.Scan() || scanner.Err() != nil {
				t.Fatalf("clean EOF: %v", scanner.Err())
			}
		} else if scanner.Scan() || scanner.Err() == nil {
			t.Fatal("oversized scanner frame accepted")
		}
	}
}

func framingServer(t *testing.T) (string, string) {
	t.Helper()
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	s := NewServer(tokens)
	s.authTimeout = 500 * time.Millisecond
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(2 * time.Second):
			t.Error("TCP shutdown timeout")
		}
	})
	return listener.Addr().String(), issueToken(t, tokens, 11, "alice")
}

func TestTCPFramingFragmentedAndCoalesced(t *testing.T) {
	address, token := framingServer(t)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	authJSON, _ := json.Marshal(map[string]string{"type": "authenticate", "token": token, "session_id": "framing-session"})
	write := func(data []byte) {
		t.Helper()
		if _, err := conn.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	write(authJSON[:17])
	_ = conn.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	if _, err := reader.Peek(1); err == nil {
		t.Fatal("partial authentication was processed")
	} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatalf("partial frame: %v", err)
	}
	// Finish authentication and append two complete pings in one write.
	write(append(append([]byte{}, authJSON[17:]...), []byte("\n{\"type\":\"ping\"}\n{\"type\":\"ping\"}\n")...))
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for _, want := range []string{"authenticated", "pong", "pong"} {
		var response map[string]string
		if err := readLineJSON(reader, &response); err != nil || response["type"] != want {
			t.Fatalf("want %s, got %v (%v)", want, response, err)
		}
	}
	write([]byte("{\"type\":"))
	write([]byte("\"ping\"}"))
	_ = conn.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	if _, err := reader.Peek(1); err == nil {
		t.Fatal("ping without newline was processed")
	} else if e, ok := err.(net.Error); !ok || !e.Timeout() {
		t.Fatal(err)
	}
	write([]byte("\n"))
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var response map[string]string
	if err := readLineJSON(reader, &response); err != nil || response["type"] != "pong" {
		t.Fatalf("fragmented ping: %v %v", response, err)
	}
}

func TestTCPFramingMissingNewlineTimesOut(t *testing.T) {
	address, token := framingServer(t)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, _ := json.Marshal(map[string]string{"type": "authenticate", "token": token, "session_id": "no-newline-session"})
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 1)
	if n, err := conn.Read(buffer); err == nil || n != 0 {
		t.Fatalf("unterminated authentication produced data: %q %v", buffer[:n], err)
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("server did not close after authentication timeout")
	}
}

func TestTCPFramingInvalidAndOversizedFrames(t *testing.T) {
	address, token := framingServer(t)
	conn, reader := connectSession(t, address, token, "bad-message-session")
	defer conn.Close()
	if _, err := conn.Write([]byte("{broken}\n{\"type\":\"ping\"}\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	for _, want := range []string{"error", "pong"} {
		var response map[string]string
		if err := readLineJSON(reader, &response); err != nil || response["type"] != want {
			t.Fatalf("want %s, got %v (%v)", want, response, err)
		}
	}
	if _, err := conn.Write([]byte(strings.Repeat("x", maxMessageBytes+1) + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("oversized frame accepted")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("oversized client was not disconnected")
	}
	// An offending connection must not break the listener for another client.
	healthy, _ := connectSession(t, address, token, "healthy-frame-session")
	healthy.Close()
}
