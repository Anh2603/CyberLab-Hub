package tcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/Anh2603/CyberLab-Hub/internal/store"
)

const maxMessageBytes = 8 * 1024

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

type ProgressEvent struct {
	Type        string    `json:"type"`
	ChallengeID string    `json:"challenge_id"`
	SolvedAt    time.Time `json:"solved_at"`
}

type Server struct {
	tokens      *auth.Manager
	authTimeout time.Duration
	idleTimeout time.Duration
	hub         *hub
	connections sync.WaitGroup
}

func NewServer(tokens *auth.Manager) *Server {
	return &Server{tokens: tokens, authTimeout: 5 * time.Second, idleTimeout: 90 * time.Second, hub: newHub()}
}

func (s *Server) ListenAndServe(ctx context.Context, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return s.Serve(ctx, listener)
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		s.hub.closeAll()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				s.connections.Wait()
				return nil
			}
			return fmt.Errorf("accept TCP progress connection: %w", err)
		}
		s.connections.Add(1)
		go func() {
			defer s.connections.Done()
			s.handleConnection(connection)
		}()
	}
}

func (s *Server) PublishProgress(userID int64, originSessionID, challengeID string, solvedAt time.Time) {
	s.hub.publish(userID, originSessionID, ProgressEvent{
		Type: "progress", ChallengeID: challengeID, SolvedAt: solvedAt.UTC(),
	})
}

func (s *Server) PublishMangaProgress(userID int64, origin string, entry store.LibraryEntry) {
	s.hub.publish(userID, origin, map[string]any{"type": "progress_updated", "manga_id": entry.MangaID, "chapter": entry.CurrentChapter, "status": entry.Status, "version": entry.Version, "updated_at": entry.UpdatedAt})
}

func (s *Server) handleConnection(connection net.Conn) {
	defer connection.Close()
	if s.tokens == nil {
		writeDirect(connection, map[string]string{"type": "error", "error": "authentication unavailable"})
		return
	}
	_ = connection.SetReadDeadline(time.Now().Add(s.authTimeout))
	scanner := bufio.NewScanner(connection)
	scanner.Buffer(make([]byte, 1024), maxMessageBytes)
	if !scanner.Scan() {
		return
	}
	var first struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &first); err != nil || first.Type != "authenticate" || !sessionIDPattern.MatchString(first.SessionID) {
		writeDirect(connection, map[string]string{"type": "error", "error": "invalid authentication message"})
		return
	}
	identity, err := s.tokens.Parse(first.Token)
	if err != nil {
		writeDirect(connection, map[string]string{"type": "error", "error": "invalid token"})
		return
	}
	client := &client{
		userID: identity.UserID, sessionID: first.SessionID, connection: connection,
		send: make(chan any, 16), done: make(chan struct{}),
	}
	if err := s.hub.add(client); err != nil {
		writeDirect(connection, map[string]string{"type": "error", "error": err.Error()})
		return
	}
	defer s.hub.remove(client)
	go client.writeLoop()
	client.queue(map[string]string{"type": "authenticated", "session_id": client.sessionID})

	for {
		_ = connection.SetReadDeadline(time.Now().Add(s.idleTimeout))
		if !scanner.Scan() {
			return
		}
		var message struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			client.queue(map[string]string{"type": "error", "error": "invalid message"})
			continue
		}
		if message.Type != "ping" {
			client.queue(map[string]string{"type": "error", "error": "unsupported message type"})
			continue
		}
		client.queue(map[string]string{"type": "pong"})
	}
}

type client struct {
	userID     int64
	sessionID  string
	connection net.Conn
	send       chan any
	done       chan struct{}
	closeOnce  sync.Once
}

func (c *client) queue(message any) bool {
	select {
	case c.send <- message:
		return true
	case <-c.done:
		return false
	default:
		c.close()
		return false
	}
}

func (c *client) writeLoop() {
	for {
		select {
		case message := <-c.send:
			_ = c.connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := writeJSONLine(c.connection, message); err != nil {
				c.close()
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.connection.Close()
	})
}

type hub struct {
	mu       sync.Mutex
	sessions map[int64]map[string]*client
}

func newHub() *hub { return &hub{sessions: make(map[int64]map[string]*client)} }

func (h *hub) add(connectionClient *client) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	byUser := h.sessions[connectionClient.userID]
	if byUser == nil {
		byUser = make(map[string]*client)
		h.sessions[connectionClient.userID] = byUser
	}
	if _, exists := byUser[connectionClient.sessionID]; exists {
		return errors.New("session id already connected")
	}
	byUser[connectionClient.sessionID] = connectionClient
	return nil
}

func (h *hub) remove(connectionClient *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if byUser := h.sessions[connectionClient.userID]; byUser != nil {
		if byUser[connectionClient.sessionID] == connectionClient {
			delete(byUser, connectionClient.sessionID)
		}
		if len(byUser) == 0 {
			delete(h.sessions, connectionClient.userID)
		}
	}
	connectionClient.close()
}

func (h *hub) publish(userID int64, originSessionID string, event any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sessionID, client := range h.sessions[userID] {
		if sessionID != originSessionID {
			client.queue(event)
		}
	}
}

func (h *hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, byUser := range h.sessions {
		for _, client := range byUser {
			client.close()
		}
	}
}

func writeDirect(connection net.Conn, message any) {
	_ = connection.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_ = writeJSONLine(connection, message)
}

func writeJSONLine(connection net.Conn, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = connection.Write(payload)
	return err
}
