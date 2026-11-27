package tui

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in because this builds and launches the real binaries. It never uses the
// user's runtime databases, accounts, ports, or environment files.
func TestMangaProcessRestart(t *testing.T) {
	if os.Getenv("MANGAHUB_PROCESS_TEST") != "1" {
		t.Skip("set MANGAHUB_PROCESS_TEST=1 for independent-process restart verification")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	build := func(name string) string {
		binary := filepath.Join(dir, name+".exe")
		cmd := exec.Command("go", "build", "-o", binary, "./cmd/"+name)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		return binary
	}
	backendBinary, catalogBinary := build("backend"), build("catalog")
	freeTCP := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		l.Close()
		return addr
	}
	httpAddr, tcpAddr, grpcAddr := freeTCP(), freeTCP(), freeTCP()
	ul, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpAddr := ul.LocalAddr().String()
	ul.Close()
	env := append(os.Environ(), "HTTP_ADDR="+httpAddr, "TCP_ADDR="+tcpAddr, "UDP_ADDR="+udpAddr, "CATALOG_LISTEN_ADDR="+grpcAddr, "CATALOG_GRPC_ADDR="+grpcAddr, "GRPC_TIMEOUT=300ms", "BACKEND_DB_PATH="+filepath.Join(dir, "backend.db"), "CATALOG_DB_PATH="+filepath.Join(dir, "catalog.db"), "JWT_SECRET=process-restart-test-secret-0123456789", "BOOTSTRAP_ADMIN_USERNAME=operator", "BOOTSTRAP_ADMIN_PASSWORD=process-admin-password")
	start := func(binary, label string) func() {
		file, err := os.Create(filepath.Join(dir, label+".log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary)
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdout = file
		cmd.Stderr = file
		if err = cmd.Start(); err != nil {
			file.Close()
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = file.Close()
		}
		t.Cleanup(stop)
		return stop
	}
	stopCatalog := start(catalogBinary, "catalog-1")
	stopBackend := start(backendBinary, "backend-1")
	base := "http://" + httpAddr
	wait := func(label string, condition func() bool) {
		until := time.Now().Add(10 * time.Second)
		for time.Now().Before(until) {
			if condition() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		for _, n := range []string{"backend-1.log", "backend-2.log", "catalog-1.log", "catalog-2.log"} {
			if data, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
				t.Logf("%s:\n%s", n, data)
			}
		}
		t.Fatal("timeout: " + label)
	}
	health := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/health", nil)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		r.Body.Close()
		return r.StatusCode == 200
	}
	wait("backend startup", health)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, _ := NewClient(base, tcpAddr, udpAddr)
	if _, err = first.Authenticate(ctx, "restart-reader", "strong-password", true); err != nil {
		t.Fatal(err)
	}
	second, _ := NewClient(base, tcpAddr, udpAddr)
	if _, err = second.Authenticate(ctx, "restart-reader", "strong-password", false); err != nil {
		t.Fatal(err)
	}
	wait("catalog startup", func() bool { items, err := first.SearchManga(ctx, ""); return err == nil && len(items) == 30 })
	if err = first.AddManga(ctx, "naruto"); err != nil {
		t.Fatal(err)
	}
	terminal := startMangaTerminal(t, second)
	terminal.do(terminal.ui.main)
	terminal.do(terminal.ui.libraryScreen)
	go second.RunReadingSync(ctx, terminal.ui.events, terminal.ui.statuses)
	if _, err = first.UpdateReading(ctx, "naruto", "reading", 10); err != nil {
		t.Fatal(err)
	}
	visible := func(chapter string) func() bool {
		return func() bool {
			if !terminal.ui.libraryVisible || terminal.ui.library.GetItemCount() != 1 {
				return false
			}
			_, text := terminal.ui.library.GetItemText(0)
			return strings.Contains(text, chapter)
		}
	}
	terminal.wait(visible("chapter 10"))
	stopBackend()
	wait("backend stopped", func() bool { return !health() })
	stopBackend = start(backendBinary, "backend-2")
	wait("backend restart", health)
	saved, err := first.Library(ctx)
	if err != nil || len(saved) != 1 || saved[0].CurrentChapter != 10 {
		t.Fatalf("backend restart lost progress: %#v %v", saved, err)
	}
	// Same JWT secret permits existing TUI sessions to resume after the restart.
	if _, err = first.UpdateReading(ctx, "naruto", "reading", 11); err != nil {
		t.Fatal(err)
	}
	terminal.wait(visible("chapter 11"))
	if !strings.Contains(terminal.snapshot(), "chapter 11") {
		t.Fatal("reconnected TUI did not render saved progress")
	}
	admin, _ := NewClient(base, tcpAddr, udpAddr)
	if _, err = admin.Authenticate(ctx, "operator", "process-admin-password", false); err != nil {
		t.Fatal(err)
	}
	if err = admin.ReleaseChapter(ctx, "one-piece", 1200); err != nil {
		t.Fatal(err)
	}
	stopCatalog()
	started := time.Now()
	if _, err = first.SearchManga(ctx, ""); err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("catalog outage was not bounded: %v", err)
	}
	stopCatalog = start(catalogBinary, "catalog-2")
	wait("catalog restart", func() bool { m, err := first.GetManga(ctx, "one-piece"); return err == nil && m.TotalChapters == 1200 })
	// Wrong password and independent account library are checked on the real process.
	stranger, _ := NewClient(base, tcpAddr, udpAddr)
	if _, err = stranger.Authenticate(ctx, "restart-reader", "wrong-password", false); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err = stranger.Authenticate(ctx, "other-reader", "strong-password", true); err != nil {
		t.Fatal(err)
	}
	private, err := stranger.Library(ctx)
	if err != nil || len(private) != 0 {
		t.Fatalf("private library leaked: %#v %v", private, err)
	}
	stopBackend()
	stopCatalog()
	t.Log("PASS: real backend/catalog stop-restart; persisted progress and chapter release; automatic TCP reconnect/resync rendered in TUI; controlled outage and account isolation")
}
