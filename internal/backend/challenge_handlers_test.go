package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/catalog"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestHTTPChallengeWorkflow(t *testing.T) {
	harness := newChallengeHarness(t, 500*time.Millisecond)
	defer harness.close()

	register := performJSON(harness.router, http.MethodPost, "/api/auth/register",
		`{"username":"learner1","password":"strong-password"}`, "")
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(register.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("registration status = %d, decode error = %v, body = %s", register.Code, err, register.Body.String())
	}

	search := performJSON(harness.router, http.MethodGet, "/api/challenges?category=log-analysis", "", session.Token)
	if search.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", search.Code, search.Body.String())
	}
	if string(search.Body.Bytes()) == "" || !bytes.Contains(search.Body.Bytes(), []byte("dns-log-basics")) {
		t.Fatalf("search body = %s", search.Body.String())
	}

	correct := performJSON(harness.router, http.MethodPost, "/api/challenges/dns-log-basics/submissions",
		`{"answer":"flag{dns_basics}"}`, session.Token)
	if correct.Code != http.StatusOK || !bytes.Contains(correct.Body.Bytes(), []byte(`"correct":true`)) {
		t.Fatalf("correct submission status = %d, body = %s", correct.Code, correct.Body.String())
	}
	wrong := performJSON(harness.router, http.MethodPost, "/api/challenges/file-signature/submissions",
		`{"answer":"flag{wrong}"}`, session.Token)
	if wrong.Code != http.StatusOK || !bytes.Contains(wrong.Body.Bytes(), []byte(`"correct":false`)) {
		t.Fatalf("wrong submission status = %d, body = %s", wrong.Code, wrong.Body.String())
	}
	if len(harness.events.challengeIDs) != 1 || harness.events.challengeIDs[0] != "dns-log-basics" {
		t.Fatalf("published progress events = %#v", harness.events.challengeIDs)
	}
	progress := performJSON(harness.router, http.MethodGet, "/api/progress", "", session.Token)
	if progress.Code != http.StatusOK || !bytes.Contains(progress.Body.Bytes(), []byte("dns-log-basics")) || bytes.Contains(progress.Body.Bytes(), []byte("file-signature")) {
		t.Fatalf("progress status = %d, body = %s", progress.Code, progress.Body.String())
	}
}

func TestAdminCanPublishAndLearnerCannot(t *testing.T) {
	harness := newChallengeHarness(t, 500*time.Millisecond)
	defer harness.close()
	learnerToken := registerToken(t, harness.router, "learner2")
	payload := `{"id":"new-log-task","title":"New Log Task","description":"Review a synthetic log.","category":"log-analysis","difficulty":"easy","answer":"flag{new_log}"}`
	denied := performJSON(harness.router, http.MethodPost, "/api/admin/challenges", payload, learnerToken)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("learner publish status = %d, want %d", denied.Code, http.StatusForbidden)
	}

	created, err := BootstrapAdmin(context.Background(), harness.accounts, "operator", "admin-password-123")
	if err != nil || !created {
		t.Fatalf("BootstrapAdmin() created = %v, error = %v", created, err)
	}
	admin, err := harness.accounts.FindUserByUsername(context.Background(), "operator")
	if err != nil {
		t.Fatalf("FindUserByUsername() error = %v", err)
	}
	adminToken, err := harness.tokens.Issue(auth.Identity{UserID: admin.ID, Username: admin.Username, Role: admin.Role})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	published := performJSON(harness.router, http.MethodPost, "/api/admin/challenges", payload, adminToken)
	if published.Code != http.StatusCreated || !bytes.Contains(published.Body.Bytes(), []byte("new-log-task")) {
		t.Fatalf("admin publish status = %d, body = %s", published.Code, published.Body.String())
	}
	if len(harness.notifications.ids) != 1 || harness.notifications.ids[0] != "new-log-task" {
		t.Fatalf("publication notifications = %#v", harness.notifications.ids)
	}
}

func TestStoppedCatalogReturnsBoundedFailure(t *testing.T) {
	harness := newChallengeHarness(t, 100*time.Millisecond)
	defer harness.close()
	token := registerToken(t, harness.router, "learner3")
	harness.grpcServer.Stop()
	started := time.Now()
	response := performJSON(harness.router, http.MethodGet, "/api/challenges", "", token)
	if response.Code != http.StatusServiceUnavailable && response.Code != http.StatusGatewayTimeout {
		t.Fatalf("stopped catalog status = %d, body = %s", response.Code, response.Body.String())
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stopped catalog response took %v, want <= 1s", elapsed)
	}
}

type challengeHarness struct {
	router        http.Handler
	accounts      *store.Backend
	tokens        *auth.Manager
	grpcServer    *grpc.Server
	connection    *grpc.ClientConn
	listener      *bufconn.Listener
	events        *eventRecorder
	notifications *notificationRecorder
}

func newChallengeHarness(t *testing.T, timeout time.Duration) *challengeHarness {
	t.Helper()
	ctx := context.Background()
	accounts, err := store.OpenBackend(ctx, filepath.Join(t.TempDir(), "backend.db"))
	if err != nil {
		t.Fatalf("OpenBackend() error = %v", err)
	}
	catalogStore, err := store.OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		accounts.Close()
		t.Fatalf("OpenCatalog() error = %v", err)
	}
	if err := catalog.SeedDefaults(ctx, catalogStore); err != nil {
		accounts.Close()
		catalogStore.Close()
		t.Fatalf("SeedDefaults() error = %v", err)
	}
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	catalogv1.RegisterCatalogServiceServer(grpcServer, catalog.NewServer(catalogStore))
	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Logf("test catalog server stopped: %v", err)
		}
	}()
	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		grpcServer.Stop()
		listener.Close()
		accounts.Close()
		catalogStore.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	tokens := auth.NewManager("01234567890123456789012345678901", time.Hour)
	events := &eventRecorder{}
	notifications := &notificationRecorder{}
	router := NewRouter(Dependencies{
		Accounts: accounts, Tokens: tokens, Catalog: catalogv1.NewCatalogServiceClient(connection),
		Progress: accounts, Events: events, Notifier: notifications, RPCTimeout: timeout,
	})
	harness := &challengeHarness{
		router: router, accounts: accounts, tokens: tokens, grpcServer: grpcServer,
		connection: connection, listener: listener,
		events:        events,
		notifications: notifications,
	}
	t.Cleanup(func() { _ = catalogStore.Close() })
	return harness
}

type eventRecorder struct {
	challengeIDs []string
}

func (r *eventRecorder) PublishProgress(_ int64, _ string, challengeID string, _ time.Time) {
	r.challengeIDs = append(r.challengeIDs, challengeID)
}

type notificationRecorder struct {
	ids []string
}

func (r *notificationRecorder) NotifyChallengePublished(id, _, _, _ string) {
	r.ids = append(r.ids, id)
}

func (h *challengeHarness) close() {
	h.connection.Close()
	h.grpcServer.Stop()
	h.listener.Close()
	h.accounts.Close()
}

func registerToken(t *testing.T, router http.Handler, username string) string {
	t.Helper()
	response := performJSON(router, http.MethodPost, "/api/auth/register",
		`{"username":"`+username+`","password":"strong-password"}`, "")
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("register %s status = %d, error = %v, body = %s", username, response.Code, err, response.Body.String())
	}
	return session.Token
}
