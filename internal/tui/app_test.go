package tui

import (
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
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

// This uses real network listeners and independent terminal event loops. It
// verifies user-visible state, rather than only checking that a send succeeded.
func TestSeparateLoginAndSignupScreens(t *testing.T) {
	h := startTerminal(t, "http://127.0.0.1:1", "127.0.0.1:1", "127.0.0.1:1")
	h.do(func() {
		_, page := h.ui.pages.GetFrontPage()
		form := page.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
		if form.GetFormItemCount() != 2 {
			t.Error("login should only request username and password")
		}
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
	})
	if text := h.snapshot(); !strings.Contains(text, "Create account") || !strings.Contains(text, "Confirm password") || !strings.Contains(text, "Back to login") {
		t.Fatalf("signup screen is incomplete:\n%s", text)
	}
	h.do(func() {
		_, page := h.ui.pages.GetFrontPage()
		form := page.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
		form.GetFormItem(0).(*tview.InputField).SetText("new-user")
		form.GetFormItem(1).(*tview.InputField).SetText("password-one")
		form.GetFormItem(2).(*tview.InputField).SetText("password-two")
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
		if h.ui.connecting || !strings.Contains(h.ui.status.GetText(true), "Passwords do not match") {
			t.Error("mismatched passwords should be rejected before a network request")
		}
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
	})
	if text := h.snapshot(); !strings.Contains(text, "Sign in") || strings.Contains(text, "Confirm password") {
		t.Fatalf("back navigation did not restore login:\n%s", text)
	}
}

func TestTerminalLearnerFlowAndLiveProgress(t *testing.T) {
	url, tcpAddr, udpAddr := startTerminalBackend(t)
	first := startTerminal(t, url, tcpAddr, udpAddr)
	first.do(func() { first.authenticate("learner", true) })
	first.wait(func() bool { name, _ := first.ui.pages.GetFrontPage(); return name == "main" })
	second := startTerminal(t, url, tcpAddr, udpAddr)
	second.do(func() { second.authenticate("learner", false) })
	second.wait(func() bool {
		return strings.Contains(second.ui.notifications.GetText(true), "TCP progress sync connected")
	})
	second.do(second.ui.openProgress)
	second.wait(func() bool { return strings.Contains(second.ui.status.GetText(true), "0 solved") })

	first.do(first.ui.openChallenges)
	first.wait(func() bool { return strings.Contains(first.ui.status.GetText(true), "Loaded 3 challenges") })
	first.do(func() {
		_, page := first.ui.pages.GetFrontPage()
		layout := page.(*tview.Flex)
		layout.InputHandler()(tcell.NewEventKey(tcell.KeyF2, 0, 0), first.focus)
		if first.ui.application.GetFocus() != layout.GetItem(1) {
			t.Error("F2 did not focus challenge results")
		}
		layout.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), first.focus)
	})
	first.wait(func() bool { name, _ := first.ui.pages.GetFrontPage(); return name == "challenge" })
	if text := first.snapshot(); !strings.Contains(text, "Synthetic DNS log") || !strings.Contains(text, "Submit") {
		t.Fatalf("challenge content or submit control missing from terminal:\n%s", text)
	}
	first.do(func() { first.submit("flag{wrong}") })
	first.wait(func() bool { return strings.Contains(first.ui.status.GetText(true), "Incorrect answer") })
	first.do(func() { first.submit("flag{dns_basics}") })
	first.wait(func() bool { return strings.Contains(first.ui.status.GetText(true), "progress saved") })
	second.wait(func() bool {
		list := second.ui.progressList
		if list == nil || list.GetItemCount() != 1 {
			return false
		}
		id, _ := list.GetItemText(0)
		return id == "dns-log-basics"
	})
	if text := second.snapshot(); !strings.Contains(text, "dns-log-basics") {
		t.Fatalf("TCP event did not update visible progress:\n%s", text)
	}
	second.do(func() {
		_, page := second.ui.pages.GetFrontPage()
		page.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, 0), second.focus)
	})
	second.wait(func() bool { name, _ := second.ui.pages.GetFrontPage(); return name == "main" })
}

func TestTerminalDiscussionPublicationAndArchive(t *testing.T) {
	url, tcpAddr, udpAddr := startTerminalBackend(t)
	first, second := startTerminal(t, url, tcpAddr, udpAddr), startTerminal(t, url, tcpAddr, udpAddr)
	first.do(func() { first.authenticate("alice", true) })
	second.do(func() { second.authenticate("bob", true) })
	for _, terminal := range []*terminalHarness{first, second} {
		terminal.wait(func() bool {
			return strings.Contains(terminal.ui.notifications.GetText(true), "UDP notifications subscribed")
		})
		terminal.do(func() {
			terminal.ui.openDiscussion()
			terminal.form().GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), terminal.focus)
		})
		terminal.wait(func() bool { return terminal.ui.roomSend != nil })
	}
	first.do(func() {
		form := first.form()
		form.GetFormItem(1).(*tview.InputField).SetText("hello from a terminal")
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), first.focus)
	})
	second.wait(func() bool {
		_, page := second.ui.pages.GetFrontPage()
		view := page.(*tview.Flex).GetItem(0).(*tview.TextView)
		return strings.Contains(view.GetText(true), "alice: hello from a terminal")
	})
	first.do(first.ui.openMain)
	first.do(first.ui.openNotifications)
	first.do(func() { first.form().GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), first.focus) })
	first.wait(func() bool {
		return strings.Contains(first.ui.notifications.GetText(true), "UDP notifications stopped")
	})

	admin := startTerminal(t, url, tcpAddr, udpAddr)
	admin.do(func() { admin.signIn("operator", "admin-password-123", false) })
	admin.wait(func() bool { name, _ := admin.ui.pages.GetFrontPage(); return name == "main" })
	admin.do(func() {
		admin.ui.openAdmin()
		_, page := admin.ui.pages.GetFrontPage()
		form := page.(*tview.Flex).GetItem(0).(*tview.Form)
		values := []string{"terminal-task", "Terminal Published Task", "Inspect synthetic bytes: 50 4B. Submit flag{zip}.", "forensics", "easy", "flag{zip}"}
		for i, value := range values {
			form.GetFormItem(i).(*tview.InputField).SetText(value)
		}
		form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), admin.focus)
	})
	admin.wait(func() bool { return strings.Contains(admin.ui.status.GetText(true), "Challenge published") })
	second.wait(func() bool {
		return strings.Contains(second.ui.notifications.GetText(true), "New challenge: Terminal Published Task")
	})
	first.do(func() {
		if strings.Contains(first.ui.notifications.GetText(true), "New challenge: Terminal Published Task") {
			t.Error("unsubscribed terminal received publication")
		}
	})
	admin.do(func() {
		_, page := admin.ui.pages.GetFrontPage()
		page.(*tview.Flex).GetItem(0).(*tview.Form).GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), admin.focus)
	})
	admin.wait(func() bool { return strings.Contains(admin.ui.status.GetText(true), "Challenge archived") })
	items, err := second.ui.client.SearchChallenges(context.Background(), "Terminal Published Task")
	if err != nil || len(items) != 0 {
		t.Fatalf("archived challenge still visible: %v, %v", items, err)
	}
}

func (h *terminalHarness) form() *tview.Form {
	_, page := h.ui.pages.GetFrontPage()
	return page.(*tview.Flex).GetItem(1).(*tview.Form)
}

type terminalHarness struct {
	t      *testing.T
	ui     *UI
	screen tcell.SimulationScreen
}

func startTerminal(t *testing.T, url, tcpAddr, udpAddr string) *terminalHarness {
	t.Helper()
	client, err := NewClient(url, tcpAddr, udpAddr)
	if err != nil {
		t.Fatal(err)
	}
	ui := NewUI(client)
	screen := tcell.NewSimulationScreen("UTF-8")
	ui.application.SetScreen(screen)
	screen.SetSize(100, 30)
	ready := make(chan struct{})
	var once sync.Once
	ui.application.SetAfterDrawFunc(func(tcell.Screen) { once.Do(func() { close(ready) }) })
	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not start")
	}
	h := &terminalHarness{t: t, ui: ui, screen: screen}
	t.Cleanup(func() {
		ui.cancel()
		ui.application.Stop()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("terminal did not stop")
		}
	})
	if text := h.snapshot(); !strings.Contains(text, "Username") || !strings.Contains(text, "Login") || !strings.Contains(text, "Sign up") || strings.Contains(text, "Confirm password") {
		t.Fatalf("login controls missing:\n%s", text)
	}
	return h
}

func (h *terminalHarness) do(action func()) { h.t.Helper(); h.ui.application.QueueUpdateDraw(action) }

func (h *terminalHarness) focus(p tview.Primitive) { h.ui.application.SetFocus(p) }

func (h *terminalHarness) wait(condition func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		h.do(func() { ready = condition() })
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("terminal condition not met:\n%s", h.snapshot())
}

func (h *terminalHarness) snapshot() string {
	var text strings.Builder
	h.do(func() {
		h.ui.application.ForceDraw()
		cells, width, height := h.screen.GetContents()
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				runes := cells[y*width+x].Runes
				if len(runes) == 0 {
					text.WriteByte(' ')
				} else {
					text.WriteRune(runes[0])
				}
			}
			text.WriteByte('\n')
		}
	})
	return text.String()
}

func (h *terminalHarness) authenticate(username string, register bool) {
	h.signIn(username, "strong-password", register)
}

func (h *terminalHarness) signIn(username, password string, register bool) {
	_, page := h.ui.pages.GetFrontPage()
	form := page.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
	if register {
		form.GetButton(1).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
		_, page = h.ui.pages.GetFrontPage()
		form = page.(*tview.Flex).GetItem(1).(*tview.Flex).GetItem(1).(*tview.Form)
		form.GetFormItem(2).(*tview.InputField).SetText(password)
	}
	form.GetFormItem(0).(*tview.InputField).SetText(username)
	form.GetFormItem(1).(*tview.InputField).SetText(password)
	form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
}

func (h *terminalHarness) submit(answer string) {
	_, page := h.ui.pages.GetFrontPage()
	form := page.(*tview.Flex).GetItem(1).(*tview.Form)
	form.GetFormItem(0).(*tview.InputField).SetText(answer)
	form.GetButton(0).InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), h.focus)
}

func startTerminalBackend(t *testing.T) (string, string, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	accounts, err := store.OpenBackend(ctx, filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { accounts.Close() })
	if _, err := backend.BootstrapAdmin(ctx, accounts, "operator", "admin-password-123"); err != nil {
		t.Fatal(err)
	}
	content, err := store.OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { content.Close() })
	if err := catalog.SeedDefaults(ctx, content); err != nil {
		t.Fatal(err)
	}
	listen := func() net.Listener {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		return listener
	}
	grpcListener := listen()
	grpcServer := grpc.NewServer()
	catalogv1.RegisterCatalogServiceServer(grpcServer, catalog.NewServer(content))
	go grpcServer.Serve(grpcListener)
	t.Cleanup(grpcServer.Stop)
	connection, err := grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	tcpServer, udpServer := tcpsync.NewServer(tokens), udpnotify.NewServer(tokens)
	tcpListener := listen()
	udpListener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	tcpDone, udpDone := make(chan error, 1), make(chan error, 1)
	go func() { tcpDone <- tcpServer.Serve(ctx, tcpListener) }()
	go func() { udpDone <- udpServer.Serve(ctx, udpListener) }()
	t.Cleanup(func() { cancel(); <-tcpDone; <-udpDone })
	server := httptest.NewServer(backend.NewRouter(backend.Dependencies{
		Accounts: accounts, Tokens: tokens, Catalog: catalogv1.NewCatalogServiceClient(connection),
		Progress: accounts, Events: tcpServer, Notifier: udpServer, Discussions: realtime.NewHub(), RPCTimeout: time.Second,
	}))
	t.Cleanup(server.Close)
	return server.URL, tcpListener.Addr().String(), udpListener.LocalAddr().String()
}
