package udp

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
)

func TestSubscribeNotifyAndUnsubscribe(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	server := NewServer(manager)
	server.ttl = time.Minute
	connection, address, stop := startServer(t, server)
	defer stop()
	defer connection.Close()
	token := issueNotificationToken(t, manager, 41, "alice")

	sendSubscription(t, connection, address, "subscribe", token, "client-1111")
	var acknowledgement map[string]any
	readDatagram(t, connection, &acknowledgement)
	if acknowledgement["type"] != "subscribed" {
		t.Fatalf("subscribe response = %#v", acknowledgement)
	}

	server.NotifyChallengePublished("new-task", "New Task", "forensics", "easy")
	var event PublicationEvent
	readDatagram(t, connection, &event)
	if event.Type != "challenge_published" || event.ID != "new-task" {
		t.Fatalf("publication event = %#v", event)
	}

	sendSubscription(t, connection, address, "unsubscribe", token, "client-1111")
	readDatagram(t, connection, &acknowledgement)
	if acknowledgement["type"] != "unsubscribed" {
		t.Fatalf("unsubscribe response = %#v", acknowledgement)
	}
	server.NotifyChallengePublished("another-task", "Another Task", "network", "medium")
	assertNoDatagram(t, connection)
}

func TestExpiredAndUnreachableSubscribersDoNotBreakDelivery(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	server := NewServer(manager)
	server.ttl = 50 * time.Millisecond
	expired, address, stop := startServer(t, server)
	defer stop()
	token := issueNotificationToken(t, manager, 42, "bob")
	sendSubscription(t, expired, address, "subscribe", token, "client-2222")
	var response map[string]any
	readDatagram(t, expired, &response)
	time.Sleep(75 * time.Millisecond)
	server.NotifyChallengePublished("expired-task", "Expired Task", "network", "easy")
	assertNoDatagram(t, expired)
	expired.Close()

	server.ttl = time.Minute
	unreachable, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP() unreachable error = %v", err)
	}
	sendSubscription(t, unreachable, address, "subscribe", token, "client-3333")
	readDatagram(t, unreachable, &response)
	unreachable.Close()

	active, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP() active error = %v", err)
	}
	defer active.Close()
	sendSubscription(t, active, address, "subscribe", token, "client-4444")
	readDatagram(t, active, &response)
	server.NotifyChallengePublished("live-task", "Live Task", "log-analysis", "hard")
	var event PublicationEvent
	readDatagram(t, active, &event)
	if event.ID != "live-task" {
		t.Fatalf("active subscriber event = %#v", event)
	}
}

func TestInvalidTokenIsRejected(t *testing.T) {
	manager := auth.NewManager("01234567890123456789012345678901", time.Hour)
	server := NewServer(manager)
	connection, address, stop := startServer(t, server)
	defer stop()
	defer connection.Close()
	sendSubscription(t, connection, address, "subscribe", "invalid", "client-5555")
	var response map[string]string
	readDatagram(t, connection, &response)
	if response["type"] != "error" || response["error"] != "invalid token" {
		t.Fatalf("invalid token response = %#v", response)
	}
}

func startServer(t *testing.T, server *Server) (*net.UDPConn, *net.UDPAddr, func()) {
	t.Helper()
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("server ListenUDP() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	client, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		cancel()
		<-done
		t.Fatalf("client ListenUDP() error = %v", err)
	}
	stop := func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}
	return client, listener.LocalAddr().(*net.UDPAddr), stop
}

func issueNotificationToken(t *testing.T, manager *auth.Manager, userID int64, username string) string {
	t.Helper()
	token, err := manager.Issue(auth.Identity{UserID: userID, Username: username, Role: "learner"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return token
}

func sendSubscription(t *testing.T, connection *net.UDPConn, address *net.UDPAddr, kind, token, clientID string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"type": kind, "token": token, "client_id": clientID,
	})
	if _, err := connection.WriteToUDP(payload, address); err != nil {
		t.Fatalf("WriteToUDP() error = %v", err)
	}
}

func readDatagram(t *testing.T, connection *net.UDPConn, destination any) {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, maxDatagramBytes)
	count, _, err := connection.ReadFromUDP(buffer)
	if err != nil {
		t.Fatalf("ReadFromUDP() error = %v", err)
	}
	if err := json.Unmarshal(buffer[:count], destination); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, payload = %s", err, buffer[:count])
	}
}

func assertNoDatagram(t *testing.T, connection *net.UDPConn) {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buffer := make([]byte, maxDatagramBytes)
	_, _, err := connection.ReadFromUDP(buffer)
	if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
		t.Fatalf("ReadFromUDP() error = %v, want timeout", err)
	}
}
