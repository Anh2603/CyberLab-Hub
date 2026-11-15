package realtime

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Anh2603/CyberLab-Hub/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	maxIncomingBytes = 4 * 1024
	maxBodyBytes     = 2 * 1024
	writeTimeout     = 5 * time.Second
	pongTimeout      = 60 * time.Second
	pingInterval     = 30 * time.Second
)

var roomPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)

type Hub struct {
	upgrader websocket.Upgrader
	mu       sync.Mutex
	rooms    map[string]map[*client]struct{}
}

func NewHub() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024},
		rooms:    make(map[string]map[*client]struct{}),
	}
}

func (h *Hub) Handle(c *gin.Context) {
	room := c.Param("room")
	if !roomPattern.MatchString(room) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid discussion room"})
		return
	}
	identity, ok := c.MustGet("identity").(auth.Identity)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	connection, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	participant := &client{
		hub: h, connection: connection, room: room, identity: identity,
		send: make(chan []byte, 32), done: make(chan struct{}),
	}
	h.register(participant)
	go participant.writePump()
	participant.readPump()
}

func (h *Hub) register(participant *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[participant.room]
	if room == nil {
		room = make(map[*client]struct{})
		h.rooms[participant.room] = room
	}
	room[participant] = struct{}{}
}

func (h *Hub) unregister(participant *client) {
	h.mu.Lock()
	if room := h.rooms[participant.room]; room != nil {
		delete(room, participant)
		if len(room) == 0 {
			delete(h.rooms, participant.room)
		}
	}
	h.mu.Unlock()
	participant.close()
}

func (h *Hub) broadcast(room string, payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for participant := range h.rooms[room] {
		participant.queue(payload)
	}
}

type client struct {
	hub        *Hub
	connection *websocket.Conn
	room       string
	identity   auth.Identity
	send       chan []byte
	done       chan struct{}
	closeOnce  sync.Once
}

func (c *client) readPump() {
	defer c.hub.unregister(c)
	c.connection.SetReadLimit(maxIncomingBytes)
	_ = c.connection.SetReadDeadline(time.Now().Add(pongTimeout))
	c.connection.SetPongHandler(func(string) error {
		return c.connection.SetReadDeadline(time.Now().Add(pongTimeout))
	})
	for {
		messageType, payload, err := c.connection.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			c.queueJSON(map[string]string{"type": "error", "error": "text messages only"})
			continue
		}
		var input struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(payload, &input); err != nil {
			c.queueJSON(map[string]string{"type": "error", "error": "invalid message"})
			continue
		}
		input.Body = strings.TrimSpace(input.Body)
		if input.Body == "" || len(input.Body) > maxBodyBytes {
			c.queueJSON(map[string]string{"type": "error", "error": "message body must contain 1-2048 bytes"})
			continue
		}
		outgoing, _ := json.Marshal(map[string]any{
			"type": "message", "room": c.room, "username": c.identity.Username,
			"body": input.Body, "sent_at": time.Now().UTC(),
		})
		c.hub.broadcast(c.room, outgoing)
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	defer c.hub.unregister(c)
	for {
		select {
		case payload := <-c.send:
			_ = c.connection.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.connection.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.connection.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.connection.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *client) queue(payload []byte) bool {
	select {
	case c.send <- payload:
		return true
	case <-c.done:
		return false
	default:
		c.close()
		return false
	}
}

func (c *client) queueJSON(message any) {
	payload, err := json.Marshal(message)
	if err == nil {
		c.queue(payload)
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.connection.Close()
	})
}
