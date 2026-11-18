package catalog

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	catalogv1 "github.com/Anh2603/CyberLab-Hub/gen/catalog/v1"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestCatalogRPCWorkflow(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	published, err := client.PublishChallenge(ctx, &catalogv1.PublishChallengeRequest{
		Id: "packet-review", Title: "Packet Review", Description: "Inspect a synthetic packet trace.",
		Category: "network", Difficulty: "easy", Answer: "FLAG{SYN_ACK}",
	})
	if err != nil {
		t.Fatalf("PublishChallenge() error = %v", err)
	}
	if !published.GetChallenge().GetPublished() || published.GetChallenge().GetId() != "packet-review" {
		t.Fatalf("published challenge = %#v", published.GetChallenge())
	}

	found, err := client.SearchChallenges(ctx, &catalogv1.SearchChallengesRequest{Query: "packet", Difficulty: "easy"})
	if err != nil || len(found.GetChallenges()) != 1 {
		t.Fatalf("SearchChallenges() count = %d, error = %v", len(found.GetChallenges()), err)
	}
	correct, err := client.VerifyAnswer(ctx, &catalogv1.VerifyAnswerRequest{ChallengeId: "packet-review", Answer: "  flag{syn_ack}  "})
	if err != nil || !correct.GetCorrect() {
		t.Fatalf("correct VerifyAnswer() = %v, error = %v", correct.GetCorrect(), err)
	}
	wrong, err := client.VerifyAnswer(ctx, &catalogv1.VerifyAnswerRequest{ChallengeId: "packet-review", Answer: "flag{wrong}"})
	if err != nil || wrong.GetCorrect() {
		t.Fatalf("wrong VerifyAnswer() = %v, error = %v", wrong.GetCorrect(), err)
	}

	archived, err := client.ArchiveChallenge(ctx, &catalogv1.ArchiveChallengeRequest{Id: "packet-review"})
	if err != nil || archived.GetChallenge().GetPublished() {
		t.Fatalf("ArchiveChallenge() response = %#v, error = %v", archived.GetChallenge(), err)
	}
	_, err = client.GetChallenge(ctx, &catalogv1.GetChallengeRequest{Id: "packet-review"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("archived GetChallenge() error = %v, want NotFound", err)
	}
	visible, err := client.GetChallenge(ctx, &catalogv1.GetChallengeRequest{Id: "packet-review", IncludeUnpublished: true})
	if err != nil || visible.GetChallenge().GetPublished() {
		t.Fatalf("admin GetChallenge() response = %#v, error = %v", visible.GetChallenge(), err)
	}
}

func TestPublishValidation(t *testing.T) {
	client, cleanup := testClient(t)
	defer cleanup()
	_, err := client.PublishChallenge(context.Background(), &catalogv1.PublishChallengeRequest{Id: "INVALID"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("PublishChallenge() error = %v, want InvalidArgument", err)
	}
}

func TestSeedDefaultsOnlyWhenEmpty(t *testing.T) {
	ctx := context.Background()
	catalogStore, err := store.OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("OpenCatalog() error = %v", err)
	}
	defer catalogStore.Close()
	for iteration := 0; iteration < 2; iteration++ {
		if err := SeedDefaults(ctx, catalogStore); err != nil {
			t.Fatalf("SeedDefaults() iteration %d error = %v", iteration, err)
		}
	}
	count, err := catalogStore.ChallengeCount(ctx)
	if err != nil || count != 3 {
		t.Fatalf("challenge count = %d, error = %v", count, err)
	}
}

func TestSeedUpgradePreservesCustomContentAndArchive(t *testing.T) {
	ctx := context.Background()
	database, err := store.OpenCatalog(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, challenge := range []store.Challenge{
		{ID: "dns-log-basics", Title: "Existing DNS", Description: "Identify the queried domain in a synthetic DNS log.", Category: "network", Difficulty: "easy", AnswerHash: []byte("existing-answer-hash")},
		{ID: "file-signature", Title: "Custom Forensics", Description: "Custom assignment: do not overwrite.", Category: "forensics", Difficulty: "hard", AnswerHash: []byte("custom-answer-hash")},
	} {
		if _, err := database.PublishChallenge(ctx, challenge); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ArchiveChallenge(ctx, "dns-log-basics"); err != nil {
		t.Fatal(err)
	}
	if err := SeedDefaults(ctx, database); err != nil {
		t.Fatal(err)
	}
	upgraded, err := database.GetChallenge(ctx, "dns-log-basics", true)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Published || upgraded.Title != "Existing DNS" || string(upgraded.AnswerHash) != "existing-answer-hash" {
		t.Fatalf("upgrade changed protected fields: %#v", upgraded)
	}
	if !strings.Contains(upgraded.Description, "query=dns-basics.example") {
		t.Fatal("legacy exercise data was not upgraded")
	}
	custom, err := database.GetChallenge(ctx, "file-signature", true)
	if err != nil || custom.Description != "Custom assignment: do not overwrite." {
		t.Fatalf("custom description changed: %#v, %v", custom, err)
	}
	count, err := database.ChallengeCount(ctx)
	if err != nil || count != 2 {
		t.Fatalf("nonempty catalog was reseeded: %d, %v", count, err)
	}
}

func testClient(t *testing.T) (catalogv1.CatalogServiceClient, func()) {
	t.Helper()
	catalogStore, err := store.OpenCatalog(context.Background(), filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("OpenCatalog() error = %v", err)
	}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	catalogv1.RegisterCatalogServiceServer(server, NewServer(catalogStore))
	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("test gRPC server stopped: %v", err)
		}
	}()
	connection, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		listener.Close()
		catalogStore.Close()
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	cleanup := func() {
		connection.Close()
		server.Stop()
		listener.Close()
		catalogStore.Close()
	}
	return catalogv1.NewCatalogServiceClient(connection), cleanup
}
