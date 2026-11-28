package tui

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Anh2603/MangaHub/internal/store"
)

func TestMangaConcurrentReleasesMatchHTTPAndUDP(t *testing.T) {
	h := startMangaBackend(t)
	ctx := context.Background()
	admin := h.client(t, "operator", false)
	addr, err := net.ResolveUDPAddr("udp", h.udp)
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer subscriber.Close()
	token, _ := admin.credentials()
	payload, _ := json.Marshal(map[string]string{"type": "subscribe", "token": token, "client_id": "release-test-client"})
	if _, err := subscriber.Write(payload); err != nil {
		t.Fatal(err)
	}
	_ = subscriber.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 8192)
	n, err := subscriber.Read(buffer)
	if err != nil || !strings.Contains(string(buffer[:n]), "subscribed") {
		t.Fatalf("subscription: %q %v", buffer[:n], err)
	}
	const first, count = 1200, 24
	start := make(chan struct{})
	succeeded := make(chan int, count)
	var wg sync.WaitGroup
	for chapter := first; chapter < first+count; chapter++ {
		wg.Add(1)
		go func(chapter int) {
			defer wg.Done()
			<-start
			var m store.Manga
			err := admin.request(ctx, http.MethodPost, "/api/admin/manga/one-piece/release", map[string]int{"total_chapters": chapter}, &m, true)
			if err != nil {
				if !strings.Contains(err.Error(), "new chapter count must exceed the current total") {
					t.Errorf("release %d: %v", chapter, err)
				}
				return
			}
			if m.ID != "one-piece" || m.TotalChapters != chapter {
				t.Errorf("HTTP release %d returned %+v", chapter, m)
			}
			succeeded <- chapter
		}(chapter)
	}
	close(start)
	wg.Wait()
	close(succeeded)
	expected := map[int]bool{}
	for chapter := range succeeded {
		expected[chapter] = true
	}
	if !expected[first+count-1] {
		t.Fatal("highest release did not succeed")
	}
	// Check each successful release's notification, regardless of UDP order.
	_ = subscriber.SetReadDeadline(time.Now().Add(2 * time.Second))
	for range len(expected) {
		n, err := subscriber.Read(buffer)
		if err != nil {
			t.Fatalf("missing local UDP release: %v; remaining=%v", err, expected)
		}
		var event struct {
			Type    string `json:"type"`
			MangaID string `json:"manga_id"`
			Chapter int    `json:"chapter"`
		}
		if err := json.Unmarshal(buffer[:n], &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "new_chapter" || event.MangaID != "one-piece" || !expected[event.Chapter] {
			t.Fatalf("unexpected/duplicate UDP event: %+v", event)
		}
		delete(expected, event.Chapter)
	}
	_ = subscriber.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, err := subscriber.Read(buffer); err == nil {
		t.Fatalf("rejected release emitted an extra notification: %s", buffer[:n])
	} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("checking for extra notifications: %v", err)
	}
	m, err := admin.GetManga(ctx, "one-piece")
	if err != nil || m.TotalChapters != first+count-1 {
		t.Fatalf("final chapter: %+v, %v", m, err)
	}
}
