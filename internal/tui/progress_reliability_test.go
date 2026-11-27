package tui

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestProgressSyncBackpressureAndCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Wait for the authentication line, then emit a burst while UI is busy.
		buf := make([]byte, 2048)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err = conn.Read(buf); err != nil {
			return
		}
		writeJSONLine(conn, map[string]string{"type": "authenticated"})
		for i := 1; i <= 20; i++ {
			if writeJSONLine(conn, map[string]any{"type": "progress_updated", "manga_id": "naruto", "chapter": i, "version": i}) != nil {
				return
			}
		}
	}()
	c, err := NewClient("http://127.0.0.1:1", listener.Addr().String(), "127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 1)
	events <- "UI busy"
	status := make(chan string, 16)
	done := make(chan struct{})
	go func() { c.RunProgressSync(ctx, events, status); close(done) }()
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("server could not send burst")
	}
	select {
	case <-done:
		t.Fatal("TCP reader exited while UI queue was full; updates were dropped")
	case <-time.After(50 * time.Millisecond):
	}
	<-events
	for i := 1; i <= 20; i++ {
		select {
		case got := <-events:
			want := fmt.Sprintf("Reading progress: naruto chapter %d (v%d)", i, i)
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("update %d lost", i)
		}
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not exit after stream closed")
	}
}

func TestProgressDeliveryCanCancelWithBusyUI(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan string, 1)
	events <- "busy"
	done := make(chan bool, 1)
	go func() { done <- deliverProgress(ctx, events, "new state") }()
	cancel()
	select {
	case sent := <-done:
		if sent {
			t.Fatal("unexpected send into full channel")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked by full UI queue")
	}
}
