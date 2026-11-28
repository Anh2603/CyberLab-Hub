package realtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func readChat(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestWebSocketRejectsMalformedMessagesAndKeepsRoomUsable(t *testing.T) {
	_, server, tokens, accounts := testDiscussionServer(t)
	defer server.Close()
	defer accounts.Close()
	token := createUserToken(t, accounts, tokens, "validation-reader")
	sender := dialRoom(t, server.URL, "general", token)
	defer sender.Close()
	peer := dialRoom(t, server.URL, "general", token)
	defer peer.Close()
	type invalidMessage struct {
		name    string
		kind    int
		payload []byte
		want    string
	}
	cases := []invalidMessage{
		{"malformed JSON", websocket.TextMessage, []byte(`{"body":`), "invalid message"},
		{"wrong body type", websocket.TextMessage, []byte(`{"body":123}`), "invalid message"},
		{"empty body", websocket.TextMessage, []byte(`{"body":"   "}`), "message body must contain 1-2048 bytes"},
		{"missing body", websocket.TextMessage, []byte(`{}`), "message body must contain 1-2048 bytes"},
		{"binary message", websocket.BinaryMessage, []byte(`{"body":"no"}`), "text messages only"},
	}
	oversize, _ := json.Marshal(map[string]string{"body": strings.Repeat("x", maxBodyBytes+1)})
	cases = append(cases, invalidMessage{"oversized body", websocket.TextMessage, oversize, "message body must contain 1-2048 bytes"})
	unicodeOversize, _ := json.Marshal(map[string]string{"body": strings.Repeat("界", 682) + "abc"})
	cases = append(cases, invalidMessage{"UTF-8 byte limit", websocket.TextMessage, unicodeOversize, "message body must contain 1-2048 bytes"})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := sender.WriteMessage(tc.kind, tc.payload); err != nil {
				t.Fatal(err)
			}
			response := readChat(t, sender)
			if response["type"] != "error" || response["error"] != tc.want {
				t.Fatalf("rejection: %v", response)
			}
			// The next item at the peer must be this valid message, never the bad input/error.
			if err := sender.WriteJSON(map[string]string{"body": tc.name}); err != nil {
				t.Fatal(err)
			}
			for _, conn := range []*websocket.Conn{sender, peer} {
				m := readChat(t, conn)
				if m["type"] != "message" || m["body"] != tc.name || m["room"] != "general" {
					t.Fatalf("room recovery: %v", m)
				}
			}
		})
	}
	// Exact byte limits are accepted, including multibyte text. Client-supplied
	// identity/room cannot override the authenticated connection's membership.
	for _, body := range []string{strings.Repeat("x", maxBodyBytes), strings.Repeat("界", 682) + "ab"} {
		if err := sender.WriteJSON(map[string]string{"body": body, "username": "forged-admin", "room": "other-room"}); err != nil {
			t.Fatal(err)
		}
		for _, conn := range []*websocket.Conn{sender, peer} {
			m := readChat(t, conn)
			if m["body"] != body || m["username"] != "validation-reader" || m["room"] != "general" {
				t.Fatalf("boundary/identity: %v", m)
			}
		}
	}
}

func TestWebSocketOversizedFrameDisconnectsOnlyOffender(t *testing.T) {
	hub, server, tokens, accounts := testDiscussionServer(t)
	defer server.Close()
	defer accounts.Close()
	token := createUserToken(t, accounts, tokens, "frame-reader")
	bad := dialRoom(t, server.URL, "general", token)
	defer bad.Close()
	healthy := dialRoom(t, server.URL, "general", token)
	defer healthy.Close()
	if err := bad.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxIncomingBytes+1))); err != nil {
		t.Fatal(err)
	}
	_ = bad.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := bad.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized frame must close with 1009, got %v", err)
	}
	until := time.Now().Add(time.Second)
	for {
		hub.mu.Lock()
		count := len(hub.rooms["general"])
		hub.mu.Unlock()
		if count == 1 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("offender was not removed from Hub")
		}
		time.Sleep(time.Millisecond)
	}
	if err := healthy.WriteJSON(map[string]string{"body": "still connected"}); err != nil {
		t.Fatal(err)
	}
	if m := readChat(t, healthy); m["body"] != "still connected" {
		t.Fatalf("healthy client affected: %v", m)
	}
}
