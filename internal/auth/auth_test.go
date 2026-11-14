package auth

import (
	"errors"
	"testing"
	"time"
)

func TestPasswordHashAndVerification(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("VerifyPassword() rejected correct password")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("VerifyPassword() accepted wrong password")
	}
}

func TestManagerIssuesAndRejectsExpiredToken(t *testing.T) {
	current := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	manager := NewManager("01234567890123456789012345678901", time.Hour)
	manager.now = func() time.Time { return current }
	token, err := manager.Issue(Identity{UserID: 7, Username: "alice", Role: "learner"})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	identity, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if identity.UserID != 7 || identity.Username != "alice" || identity.Role != "learner" {
		t.Fatalf("identity = %#v", identity)
	}
	manager.now = func() time.Time { return current.Add(2 * time.Hour) }
	if _, err := manager.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired Parse() error = %v, want ErrInvalidToken", err)
	}
}
