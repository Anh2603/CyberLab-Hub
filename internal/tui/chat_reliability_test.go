package tui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gorilla/websocket"
	"github.com/rivo/tview"
)

func TestChatBusyQueuePreservesMessagesAndReportsDisconnect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for i := 0; i < 20; i++ {
			if err := conn.WriteJSON(map[string]any{"type": "message", "username": "alice", "body": fmt.Sprintf("message-%02d", i), "sent_at": time.Now().UTC()}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "127.0.0.1:1", "127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := make(chan string, 1)
	send, err := c.JoinRoom(ctx, "general", events)
	if err != nil {
		t.Fatal(err)
	}
	// Do not drain until the first slot fills: exercise a slow UI with a burst.
	deadline := time.Now().Add(time.Second)
	for len(events) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		select {
		case value, ok := <-events:
			if !ok || !strings.Contains(value, fmt.Sprintf("alice: message-%02d", i)) {
				t.Fatalf("message %d missing/out of order: %q", i, value)
			}
		case <-ctx.Done():
			t.Fatal("timed out receiving chat")
		}
	}
	select {
	case value := <-events:
		if !strings.HasPrefix(value, "Discussion disconnected") {
			t.Fatalf("missing disconnect: %q", value)
		}
	case <-ctx.Done():
		t.Fatal("missing disconnect event")
	}
	// The reader cancels the connection after reporting the disconnect.
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("reader did not close")
	}
	if err := send("draft"); err == nil {
		t.Fatal("disconnected send accepted")
	}
}

func TestMangaTerminalFailedSendKeepsDraft(t *testing.T) {
	h := startMangaBackend(t)
	c := h.client(t, "draft-reader", true)
	term := startMangaTerminal(t, c)
	var input *tview.InputField
	term.do(func() {
		term.ui.discussion()
		_, p := term.ui.pages.GetFrontPage()
		layout := p.(*tview.Flex).GetItem(0).(*tview.Flex)
		controls := layout.GetItem(1).(*tview.Form)
		input = layout.GetItem(3).(*tview.InputField)
		controls.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { term.ui.app.SetFocus(p) })
	})
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "Connected to general") })
	draft := strings.Repeat("界", 700)
	term.do(func() {
		input.SetText(draft)
		input.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), func(p tview.Primitive) { term.ui.app.SetFocus(p) })
	})
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "Send failed; draft kept") })
	term.do(func() {
		if input.GetText() != draft {
			t.Error("failed send erased draft")
		}
	})
}

func TestMangaTerminalMutationGuardUnlocksAfterFailure(t *testing.T) {
	c, _ := NewClient("http://127.0.0.1:1", "127.0.0.1:1", "127.0.0.1:1")
	term := startMangaTerminal(t, c)
	finish := make(chan struct{})
	calls := make(chan struct{}, 2)
	work := func() error { calls <- struct{}{}; <-finish; return errors.New("test failure") }
	term.do(func() { term.ui.mutate(work, nil); term.ui.mutate(work, nil) })
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("mutation not started")
	}
	select {
	case <-calls:
		t.Fatal("duplicate mutation started")
	default:
	}
	close(finish)
	term.wait(func() bool { return !term.ui.busy && strings.Contains(term.ui.status.GetText(true), "test failure") })
}

func TestChatFullQueueCancelsAndRejectsOversizeDraft(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteJSON(map[string]any{"type": "message", "username": "alice", "body": "busy", "sent_at": time.Now().UTC()})
		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "127.0.0.1:1", "127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string)
	send, err := c.JoinRoom(ctx, "general", events)
	if err != nil {
		t.Fatal(err)
	}
	if err := send(strings.Repeat("界", 700)); err == nil {
		t.Fatal("oversized UTF-8 draft accepted")
	}
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("unexpected message after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("full queue prevented cancellation")
	}
}
