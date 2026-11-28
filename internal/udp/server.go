package udp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
)

const maxDatagramBytes = 8 * 1024

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

type PublicationEvent struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	Difficulty string `json:"difficulty"`
}

type subscription struct {
	address   *net.UDPAddr
	expiresAt time.Time
}

type Server struct {
	tokens *auth.Manager
	ttl    time.Duration
	now    func() time.Time

	mu            sync.Mutex
	connection    *net.UDPConn
	subscriptions map[string]subscription
}

func NewServer(tokens *auth.Manager) *Server {
	return &Server{
		tokens: tokens, ttl: 2 * time.Minute, now: time.Now,
		subscriptions: make(map[string]subscription),
	}
}

func (s *Server) ListenAndServe(ctx context.Context, address string) error {
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return err
	}
	connection, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		return err
	}
	return s.Serve(ctx, connection)
}

func (s *Server) Serve(ctx context.Context, connection *net.UDPConn) error {
	s.mu.Lock()
	s.connection = connection
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.connection == connection {
			s.connection = nil
		}
		s.mu.Unlock()
	}()
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()

	buffer := make([]byte, maxDatagramBytes+1)
	for {
		count, address, err := connection.ReadFromUDP(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, syscall.ECONNRESET) {
				continue
			}
			return fmt.Errorf("read UDP notification datagram: %w", err)
		}
		if count > maxDatagramBytes {
			s.respond(connection, address, map[string]string{"type": "error", "error": "datagram too large"})
			continue
		}
		s.handleDatagram(connection, address, buffer[:count])
	}
}

func (s *Server) handleDatagram(connection *net.UDPConn, address *net.UDPAddr, payload []byte) {
	var message struct {
		Type     string `json:"type"`
		Token    string `json:"token"`
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(payload, &message); err != nil || !clientIDPattern.MatchString(message.ClientID) {
		s.respond(connection, address, map[string]string{"type": "error", "error": "invalid subscription message"})
		return
	}
	if message.Type != "subscribe" && message.Type != "unsubscribe" {
		s.respond(connection, address, map[string]string{"type": "error", "error": "unsupported message type"})
		return
	}
	if s.tokens == nil {
		s.respond(connection, address, map[string]string{"type": "error", "error": "authentication unavailable"})
		return
	}
	identity, err := s.tokens.Parse(message.Token)
	if err != nil {
		s.respond(connection, address, map[string]string{"type": "error", "error": "invalid token"})
		return
	}
	key := strconv.FormatInt(identity.UserID, 10) + ":" + message.ClientID
	now := s.now().UTC()
	s.mu.Lock()
	if message.Type == "subscribe" {
		s.subscriptions[key] = subscription{address: cloneAddress(address), expiresAt: now.Add(s.ttl)}
	} else {
		delete(s.subscriptions, key)
	}
	s.mu.Unlock()
	if message.Type == "subscribe" {
		s.respond(connection, address, map[string]any{
			"type": "subscribed", "client_id": message.ClientID, "expires_at": now.Add(s.ttl),
		})
		return
	}
	s.respond(connection, address, map[string]string{"type": "unsubscribed", "client_id": message.ClientID})
}

func (s *Server) NotifyChallengePublished(id, title, category, difficulty string) {
	s.notify(PublicationEvent{Type: "challenge_published", ID: id, Title: title, Category: category, Difficulty: difficulty})
}

func (s *Server) NotifyNewChapter(id, title string, chapter int) {
	s.notify(map[string]any{"type": "new_chapter", "manga_id": id, "title": title, "chapter": chapter})
}

func (s *Server) notify(event any) {
	now := s.now().UTC()
	s.mu.Lock()
	connection := s.connection
	addresses := make([]*net.UDPAddr, 0, len(s.subscriptions))
	for key, subscriber := range s.subscriptions {
		if !subscriber.expiresAt.After(now) {
			delete(s.subscriptions, key)
			continue
		}
		addresses = append(addresses, cloneAddress(subscriber.address))
	}
	s.mu.Unlock()
	if connection == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	for _, address := range addresses {
		_, _ = connection.WriteToUDP(payload, address)
	}
}

func (s *Server) respond(connection *net.UDPConn, address *net.UDPAddr, message any) {
	payload, err := json.Marshal(message)
	if err != nil || len(payload) > maxDatagramBytes {
		return
	}
	_, _ = connection.WriteToUDP(payload, address)
}

func cloneAddress(address *net.UDPAddr) *net.UDPAddr {
	return &net.UDPAddr{IP: append(net.IP(nil), address.IP...), Port: address.Port, Zone: address.Zone}
}
