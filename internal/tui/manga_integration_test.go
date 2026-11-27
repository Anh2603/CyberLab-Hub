package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/backend"
	"github.com/Anh2603/CyberLab-Hub/internal/catalog"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	tcpsync "github.com/Anh2603/CyberLab-Hub/internal/tcp"
	udpnotify "github.com/Anh2603/CyberLab-Hub/internal/udp"
	realtime "github.com/Anh2603/CyberLab-Hub/internal/websocket"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mangaHarness struct {
	url, tcp, udp            string
	catalog                  *grpc.Server
	backendPath, catalogPath string
	accounts                 *store.Backend
	content                  *store.Catalog
}

func startMangaBackend(t *testing.T) *mangaHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &mangaHarness{backendPath: filepath.Join(t.TempDir(), "backend.db"), catalogPath: filepath.Join(t.TempDir(), "catalog.db")}
	var err error
	h.accounts, err = store.OpenBackend(ctx, h.backendPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.accounts.Close() })
	if _, err = backend.BootstrapAdmin(ctx, h.accounts, "operator", "admin-password-123"); err != nil {
		t.Fatal(err)
	}
	h.content, err = store.OpenCatalog(ctx, h.catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.content.Close() })
	if err = catalog.SeedManga(ctx, h.content); err != nil {
		t.Fatal(err)
	}
	listen := func() net.Listener {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	gl := listen()
	h.catalog = grpc.NewServer()
	pb.RegisterMangaCatalogServer(h.catalog, catalog.NewMangaServer(h.content))
	go h.catalog.Serve(gl)
	t.Cleanup(h.catalog.Stop)
	gc, err := grpc.NewClient(gl.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gc.Close() })
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	tcp, udp := tcpsync.NewServer(tokens), udpnotify.NewServer(tokens)
	tl := listen()
	ul, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	td, ud := make(chan error, 1), make(chan error, 1)
	go func() { td <- tcp.Serve(ctx, tl) }()
	go func() { ud <- udp.Serve(ctx, ul) }()
	t.Cleanup(func() { cancel(); <-td; <-ud })
	server := httptest.NewServer(backend.NewRouter(backend.Dependencies{Accounts: h.accounts, Tokens: tokens, Manga: pb.NewMangaCatalogClient(gc), Library: h.accounts, MangaEvents: tcp, ChapterAlerts: udp, Discussions: realtime.NewHub(), RPCTimeout: 300 * time.Millisecond}))
	t.Cleanup(server.Close)
	h.url, h.tcp, h.udp = server.URL, tl.Addr().String(), ul.LocalAddr().String()
	return h
}
func (h *mangaHarness) client(t *testing.T, name string, register bool) *Client {
	t.Helper()
	c, err := NewClient(h.url, h.tcp, h.udp)
	if err != nil {
		t.Fatal(err)
	}
	password := "strong-password"
	if name == "operator" {
		password = "admin-password-123"
	}
	if _, err = c.Authenticate(context.Background(), name, password, register); err != nil {
		t.Fatal(err)
	}
	return c
}
func TestMangaConnectedWorkflowAndIsolation(t *testing.T) {
	h := startMangaBackend(t)
	ctx := context.Background()
	a := h.client(t, "alice", true)
	b := h.client(t, "alice", false)
	other := h.client(t, "charlie", true)
	admin := h.client(t, "operator", false)
	items, err := a.SearchManga(ctx, "")
	if err != nil || len(items) != 30 {
		t.Fatalf("catalog: %d %v", len(items), err)
	}
	genres := map[string]bool{}
	for _, m := range items {
		if m.Author == "" || m.Description == "" || m.TotalChapters <= 0 {
			t.Fatal("incomplete catalog metadata")
		}
		for _, g := range m.Genres {
			genres[g] = true
		}
	}
	if len(genres) < 4 {
		t.Fatal("fewer than four genres")
	}
	empty, err := a.SearchManga(ctx, "no-such-title")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty search: %v %v", empty, err)
	}
	if _, err = a.GetManga(ctx, "unknown-manga"); err == nil {
		t.Fatal("unknown id accepted")
	}
	if err = a.AddManga(ctx, "naruto"); err != nil {
		t.Fatal(err)
	}
	if err = a.AddManga(ctx, "naruto"); err == nil {
		t.Fatal("duplicate library entry accepted")
	}
	if err = other.AddManga(ctx, "one-piece"); err != nil {
		t.Fatal(err)
	}
	// Authenticate three raw TCP sessions and assert the actual framed payload.
	connect := func(c *Client) (net.Conn, *bufio.Reader) {
		conn, err := net.Dial("tcp", h.tcp)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		token, _ := c.credentials()
		if err := writeJSONLine(conn, map[string]string{"type": "authenticate", "token": token, "session_id": c.sessionID}); err != nil {
			t.Fatal(err)
		}
		r := bufio.NewReader(conn)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		line, err := r.ReadString('\n')
		if err != nil || !strings.Contains(line, "authenticated") {
			t.Fatalf("TCP ack %q %v", line, err)
		}
		return conn, r
	}
	ca, ra := connect(a)
	cb, rb := connect(b)
	cc, rc := connect(other)
	entry, err := a.UpdateReading(ctx, "naruto", "reading", 5)
	if err != nil || entry.Version != 2 {
		t.Fatalf("update: %#v %v", entry, err)
	}
	cb.SetReadDeadline(time.Now().Add(time.Second))
	line, err := rb.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type, MangaID, Status string
		Chapter               int
		Version               int64
	}
	var payload map[string]any
	if err = json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatal(err)
	}
	_ = event
	if payload["type"] != "progress_updated" || payload["manga_id"] != "naruto" || payload["chapter"] != float64(5) || payload["version"] != float64(2) {
		t.Fatalf("TCP event: %s", line)
	}
	for _, pair := range []struct {
		c net.Conn
		r *bufio.Reader
	}{{ca, ra}, {cc, rc}} {
		pair.c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if msg, err := pair.r.ReadString('\n'); err == nil {
			t.Fatalf("private/origin event leaked: %s", msg)
		}
	}
	if _, err = a.UpdateReading(ctx, "naruto", "reading", 701); err == nil {
		t.Fatal("chapter above total accepted")
	}
	if _, err = a.UpdateReading(ctx, "naruto", "reading", -1); err == nil {
		t.Fatal("negative chapter accepted")
	}
	if _, err = a.UpdateReading(ctx, "naruto", "invented", 1); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, err = other.UpdateReading(ctx, "naruto", "completed", 5); err == nil {
		t.Fatal("other user's library changed")
	}
	if err = a.ReleaseChapter(ctx, "naruto", 701); err == nil {
		t.Fatal("non-admin release accepted")
	}
	if _, err = other.requestTest(ctx, http.MethodGet, "/api/library"); err != nil {
		t.Fatal(err)
	}
	// UDP subscription, release via HTTP -> real gRPC -> catalog SQLite -> datagram.
	subscribe := func(c *Client, sub bool) *net.UDPConn {
		addr, _ := net.ResolveUDPAddr("udp", h.udp)
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if sub {
			token, _ := c.credentials()
			p, _ := json.Marshal(map[string]string{"type": "subscribe", "client_id": randomID("udp-"), "token": token})
			conn.Write(p)
			conn.SetReadDeadline(time.Now().Add(time.Second))
			buf := make([]byte, 8192)
			n, err := conn.Read(buf)
			if err != nil || !strings.Contains(string(buf[:n]), "subscribed") {
				t.Fatalf("UDP ack %s %v", buf[:n], err)
			}
		}
		return conn
	}
	ua, ub, uc := subscribe(a, true), subscribe(b, true), subscribe(other, false)
	if err = admin.ReleaseChapter(ctx, "naruto", 701); err != nil {
		t.Fatal(err)
	}
	for _, conn := range []*net.UDPConn{ua, ub} {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 8192)
		n, err := conn.Read(buf)
		if err != nil || !strings.Contains(string(buf[:n]), `"chapter":701`) {
			t.Fatalf("UDP release: %s %v", buf[:n], err)
		}
	}
	uc.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err = uc.Read(make([]byte, 8192)); err == nil {
		t.Fatal("unsubscribed client received notification")
	}
	if err = admin.ReleaseChapter(ctx, "naruto", 701); err == nil {
		t.Fatal("non-increasing release accepted")
	}
	// Existing TCP sockets can close; failed receiver must not prevent later writes.
	cb.Close()
	if _, err = a.UpdateReading(ctx, "naruto", "completed", 701); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.OpenBackend(ctx, h.backendPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	entries, err := restarted.ListLibrary(ctx, a.User().ID)
	if err != nil || len(entries) != 1 || entries[0].CurrentChapter != 701 || entries[0].Version != 3 {
		t.Fatalf("restart progress: %#v %v", entries, err)
	}
	catalogRestart, err := store.OpenCatalog(ctx, h.catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer catalogRestart.Close()
	if err = catalog.SeedManga(ctx, catalogRestart); err != nil {
		t.Fatal(err)
	}
	m, err := catalogRestart.GetManga(ctx, "naruto")
	if err != nil || m.TotalChapters != 701 {
		t.Fatalf("release lost at restart: %#v %v", m, err)
	}
	h.catalog.Stop()
	started := time.Now()
	if _, err = a.SearchManga(ctx, ""); err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("unbounded catalog error %v", err)
	}
}
func (c *Client) requestTest(ctx context.Context, method, path string) (map[string]any, error) {
	var result map[string]any
	err := c.request(ctx, method, path, nil, &result, true)
	return result, err
}

type mangaTerminal struct {
	t      *testing.T
	ui     *MangaUI
	screen tcell.SimulationScreen
}

func startMangaTerminal(t *testing.T, c *Client) *mangaTerminal {
	t.Helper()
	ui := NewMangaUI(c)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	screen.SetSize(110, 35)
	ui.app.SetScreen(screen)
	var once sync.Once
	ready := make(chan struct{})
	ui.app.SetAfterDrawFunc(func(tcell.Screen) { once.Do(func() { close(ready) }) })
	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("TUI startup timeout")
	}
	h := &mangaTerminal{t, ui, screen}
	t.Cleanup(func() {
		ui.cancel()
		ui.app.Stop()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("TUI stop timeout")
		}
	})
	return h
}
func (h *mangaTerminal) do(f func()) { h.t.Helper(); h.ui.app.QueueUpdateDraw(f) }
func (h *mangaTerminal) wait(f func() bool) {
	h.t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		var yes bool
		h.do(func() { yes = f() })
		if yes {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("TUI timeout: %s", h.snapshot())
}
func (h *mangaTerminal) snapshot() string {
	var b strings.Builder
	h.do(func() {
		h.ui.app.ForceDraw()
		cells, w, height := h.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < w; x++ {
				r := cells[y*w+x].Runes
				if len(r) > 0 {
					b.WriteRune(r[0])
				} else {
					b.WriteByte(' ')
				}
			}
			b.WriteByte('\n')
		}
	})
	return b.String()
}
func TestMangaTerminalVisibleProgressAndNotifications(t *testing.T) {
	h := startMangaBackend(t)
	a := h.client(t, "reader", true)
	b := h.client(t, "reader", false)
	admin := h.client(t, "operator", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.AddManga(ctx, "naruto"); err != nil {
		t.Fatal(err)
	}
	terminal := startMangaTerminal(t, b)
	terminal.do(terminal.ui.main)
	terminal.do(terminal.ui.libraryScreen)
	terminal.wait(func() bool { return terminal.ui.library.GetItemCount() == 1 })
	syncStatus := make(chan string, 16)
	go b.RunProgressSync(ctx, terminal.ui.events, syncStatus)
	waitForEvent(t, syncStatus, "TCP progress sync connected")
	if _, err := a.UpdateReading(ctx, "naruto", "reading", 7); err != nil {
		t.Fatal(err)
	}
	terminal.wait(func() bool {
		_, text := terminal.ui.library.GetItemText(0)
		return strings.Contains(text, "chapter 7") && strings.Contains(text, "version 2")
	})
	if !strings.Contains(terminal.snapshot(), "chapter 7") {
		t.Fatal("TCP event not visible in TUI")
	}
	terminal.do(terminal.ui.notificationScreen)
	udpStatus := make(chan string, 16)
	go b.RunNotifications(ctx, terminal.ui.events, udpStatus)
	for {
		select {
		case s := <-udpStatus:
			if s == "UDP notifications subscribed" {
				goto subscribed
			}
		case <-time.After(3 * time.Second):
			t.Fatal("UDP subscribe timeout")
		}
	}
subscribed:
	if err := admin.ReleaseChapter(ctx, "naruto", 701); err != nil {
		t.Fatal(err)
	}
	terminal.wait(func() bool { return strings.Contains(terminal.ui.alerts.GetText(true), "New chapter: Naruto") })
	terminal.do(terminal.ui.browser)
	terminal.wait(func() bool { return strings.Contains(terminal.ui.status.GetText(true), "30 manga found") })
	if !strings.Contains(terminal.snapshot(), "20th Century Boys") {
		t.Fatal("catalog browser not rendered")
	}
	terminal.do(func() { terminal.ui.details("naruto") })
	terminal.wait(func() bool { name, _ := terminal.ui.pages.GetFrontPage(); return name == "details" })
	if text := terminal.snapshot(); !strings.Contains(text, "Total chapters: 701") || !strings.Contains(text, "Add to Library") {
		t.Fatalf("missing manga details: %s", text)
	}
}
func TestMangaDiscussionRoomIsolation(t *testing.T) {
	h := startMangaBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, b, c := h.client(t, "alice", true), h.client(t, "bob", true), h.client(t, "charlie", true)
	ea, eb, ec := make(chan string, 8), make(chan string, 8), make(chan string, 8)
	sendA, err := a.JoinRoom(ctx, "general", ea)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.JoinRoom(ctx, "general", eb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.JoinRoom(ctx, "mystery", ec)
	if err != nil {
		t.Fatal(err)
	}
	if err = sendA("MangaHub live chat"); err != nil {
		t.Fatal(err)
	}
	for _, events := range []chan string{ea, eb} {
		select {
		case msg := <-events:
			if !strings.Contains(msg, "alice: MangaHub live chat") {
				t.Fatal(msg)
			}
		case <-time.After(time.Second):
			t.Fatal("chat not delivered")
		}
	}
	select {
	case msg := <-ec:
		t.Fatalf("room isolation failed: %s", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMangaTerminalSignupAddAndUpdateControls(t *testing.T) {
	h := startMangaBackend(t)
	c, err := NewClient(h.url, h.tcp, h.udp)
	if err != nil {
		t.Fatal(err)
	}
	term := startMangaTerminal(t, c)
	focus := func(p tview.Primitive) { term.ui.app.SetFocus(p) }
	click := func(button *tview.Button) {
		button.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), focus)
	}
	term.do(func() {
		_, p := term.ui.pages.GetFrontPage()
		f := p.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
		click(f.GetButton(1))
	})
	term.do(func() {
		_, p := term.ui.pages.GetFrontPage()
		f := p.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
		f.GetFormItem(0).(*tview.InputField).SetText("ui-reader")
		f.GetFormItem(1).(*tview.InputField).SetText("strong-password")
		f.GetFormItem(2).(*tview.InputField).SetText("strong-password")
		click(f.GetButton(0))
	})
	term.wait(func() bool { name, _ := term.ui.pages.GetFrontPage(); return name == "main" })
	term.do(func() { term.ui.details("naruto") })
	term.wait(func() bool { name, _ := term.ui.pages.GetFrontPage(); return name == "details" })
	term.do(func() {
		_, p := term.ui.pages.GetFrontPage()
		f := p.(*tview.Flex).GetItem(0).(*tview.Flex).GetItem(1).(*tview.Form)
		click(f.GetButton(0))
	})
	term.wait(func() bool { return strings.Contains(term.ui.status.GetText(true), "Added to your library") })
	term.do(term.ui.libraryScreen)
	term.wait(func() bool { return term.ui.library.GetItemCount() == 1 })
	term.do(func() { term.ui.library.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), focus) })
	term.wait(func() bool { name, _ := term.ui.pages.GetFrontPage(); return name == "details" })
	term.do(func() {
		_, p := term.ui.pages.GetFrontPage()
		f := p.(*tview.Flex).GetItem(0).(*tview.Form)
		f.GetFormItemByLabel("Status").(*tview.DropDown).SetCurrentOption(1)
		f.GetFormItemByLabel("Current chapter").(*tview.InputField).SetText("8")
		click(f.GetButton(0))
	})
	term.wait(func() bool {
		if !term.ui.libraryVisible || term.ui.library.GetItemCount() != 1 {
			return false
		}
		_, s := term.ui.library.GetItemText(0)
		return strings.Contains(s, "chapter 8") && strings.Contains(s, "reading")
	})
}
