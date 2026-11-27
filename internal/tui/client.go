package tui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type Challenge struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Published   bool   `json:"published"`
}

type Progress struct {
	ChallengeID string    `json:"challenge_id"`
	SolvedAt    time.Time `json:"solved_at"`
}

type Client struct {
	baseURL   string
	tcpAddr   string
	udpAddr   string
	http      *http.Client
	sessionID string

	mu    sync.RWMutex
	token string
	user  User
}

func NewClient(baseURL, tcpAddr, udpAddr string) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("invalid backend URL")
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"), tcpAddr: tcpAddr, udpAddr: udpAddr,
		http: &http.Client{Timeout: 5 * time.Second}, sessionID: randomID("session-"),
	}, nil
}

func (c *Client) Authenticate(ctx context.Context, username, password string, register bool) (User, error) {
	path := "/api/auth/login"
	if register {
		path = "/api/auth/register"
	}
	var response struct {
		Token string `json:"token"`
		User  User   `json:"user"`
	}
	if err := c.request(ctx, http.MethodPost, path, map[string]string{
		"username": username, "password": password,
	}, &response, false); err != nil {
		return User{}, err
	}
	c.mu.Lock()
	c.token, c.user = response.Token, response.User
	c.mu.Unlock()
	return response.User, nil
}

func (c *Client) User() User {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.user
}

func (c *Client) SearchChallenges(ctx context.Context, query string) ([]Challenge, error) {
	var response struct {
		Challenges []Challenge `json:"challenges"`
	}
	path := "/api/challenges"
	if query != "" {
		path += "?query=" + url.QueryEscape(query)
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &response, true); err != nil {
		return nil, err
	}
	return response.Challenges, nil
}

func (c *Client) SubmitAnswer(ctx context.Context, challengeID, answer string) (bool, error) {
	var response struct {
		Correct bool `json:"correct"`
	}
	err := c.request(ctx, http.MethodPost, "/api/challenges/"+url.PathEscape(challengeID)+"/submissions",
		map[string]string{"answer": answer}, &response, true)
	return response.Correct, err
}

func (c *Client) Progress(ctx context.Context) ([]Progress, error) {
	var response struct {
		Progress []Progress `json:"progress"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/progress", nil, &response, true); err != nil {
		return nil, err
	}
	return response.Progress, nil
}

func (c *Client) PublishChallenge(ctx context.Context, challenge Challenge, answer string) error {
	payload := map[string]string{
		"id": challenge.ID, "title": challenge.Title, "description": challenge.Description,
		"category": challenge.Category, "difficulty": challenge.Difficulty, "answer": answer,
	}
	return c.request(ctx, http.MethodPost, "/api/admin/challenges", payload, &Challenge{}, true)
}

func (c *Client) ArchiveChallenge(ctx context.Context, id string) error {
	return c.request(ctx, http.MethodDelete, "/api/admin/challenges/"+url.PathEscape(id), nil, &Challenge{}, true)
}

func (c *Client) RunProgressSync(ctx context.Context, events chan<- string, status chan<- string) {
	c.runProgressSession(ctx, events, status, false)
}

// Reconnect only on transport failure. A successful handshake invalidates local
// progress, covering updates missed while disconnected without periodic polling.
func (c *Client) RunReadingSync(ctx context.Context, events chan<- string, status chan<- string) {
	delay := 250 * time.Millisecond
	for ctx.Err() == nil {
		started := time.Now()
		c.runProgressSession(ctx, events, status, true)
		if ctx.Err() != nil {
			return
		}
		nonBlocking(status, "TCP disconnected; reconnecting and restoring saved progress")
		if time.Since(started) > 10*time.Second {
			delay = 250 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}
}

func (c *Client) runProgressSession(ctx context.Context, events chan<- string, status chan<- string, resync bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", c.tcpAddr)
	if err != nil {
		nonBlocking(status, "TCP offline: "+err.Error())
		return
	}
	defer connection.Close()
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	token, _ := c.credentials()
	if err := writeJSONLine(connection, map[string]string{
		"type": "authenticate", "token": token, "session_id": c.sessionID,
	}); err != nil {
		nonBlocking(status, "TCP authentication write failed")
		return
	}
	reader := bufio.NewScanner(connection)
	reader.Buffer(make([]byte, 1024), 8*1024)
	if !reader.Scan() {
		nonBlocking(status, "TCP authentication failed")
		return
	}
	var acknowledgement struct {
		Type  string `json:"type"`
		Error string `json:"error"`
	}
	if json.Unmarshal(reader.Bytes(), &acknowledgement) != nil || acknowledgement.Type != "authenticated" {
		if acknowledgement.Error == "" {
			acknowledgement.Error = "invalid acknowledgement"
		}
		nonBlocking(status, "TCP authentication failed: "+acknowledgement.Error)
		return
	}
	_ = connection.SetDeadline(time.Time{})
	nonBlocking(status, "TCP progress sync connected")
	if resync && !deliverProgress(ctx, events, "Reading progress: connection ready; reload saved library") {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := writeJSONLine(connection, map[string]string{"type": "ping"}); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	for reader.Scan() {
		var message struct {
			Type        string `json:"type"`
			ChallengeID string `json:"challenge_id"`
			MangaID     string `json:"manga_id"`
			Chapter     int    `json:"chapter"`
			Version     int64  `json:"version"`
		}
		if json.Unmarshal(reader.Bytes(), &message) == nil {
			if message.Type == "progress" {
				if !deliverProgress(ctx, events, "Progress synchronized: "+message.ChallengeID) {
					return
				}
			}
			if message.Type == "progress_updated" {
				if !deliverProgress(ctx, events, fmt.Sprintf("Reading progress: %s chapter %d (v%d)", message.MangaID, message.Chapter, message.Version)) {
					return
				}
			}
		}
	}
	if ctx.Err() == nil && !resync {
		nonBlocking(status, "TCP progress sync disconnected; sign in again to reconnect")
	}
}

func (c *Client) RunNotifications(ctx context.Context, events chan<- string, status chan<- string) error {
	server, err := net.ResolveUDPAddr("udp", c.udpAddr)
	if err != nil {
		nonBlocking(status, "UDP address invalid")
		return fmt.Errorf("UDP address invalid: %w", err)
	}
	connection, err := net.DialUDP("udp", nil, server)
	if err != nil {
		nonBlocking(status, "UDP unavailable: "+err.Error())
		return fmt.Errorf("UDP unavailable: %w", err)
	}
	defer connection.Close()
	token, _ := c.credentials()
	clientID := randomID("client-")
	send := func(kind string) {
		payload, _ := json.Marshal(map[string]string{"type": kind, "token": token, "client_id": clientID})
		_, _ = connection.Write(payload)
	}
	send("subscribe")
	defer func() {
		send("unsubscribe")
		_ = connection.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		buffer := make([]byte, 8*1024)
		for {
			count, err := connection.Read(buffer)
			if err != nil {
				break
			}
			var response struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(buffer[:count], &response) == nil && response.Type == "unsubscribed" {
				break
			}
		}
		nonBlocking(status, "UDP notifications stopped")
	}()
	nonBlocking(status, "UDP subscription requested")
	nextRefresh := time.Now().Add(time.Minute)
	buffer := make([]byte, 8*1024)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		count, err := connection.Read(buffer)
		if err == nil {
			var message struct {
				Type    string `json:"type"`
				Title   string `json:"title"`
				Error   string `json:"error"`
				Chapter int    `json:"chapter"`
			}
			if json.Unmarshal(buffer[:count], &message) == nil {
				switch message.Type {
				case "new_chapter":
					nonBlocking(events, fmt.Sprintf("New chapter: %s — chapter %d", message.Title, message.Chapter))
				case "challenge_published":
					nonBlocking(events, "New challenge: "+message.Title)
				case "subscribed":
					nonBlocking(status, "UDP notifications subscribed")
				case "error":
					nonBlocking(status, "UDP: "+message.Error)
					return errors.New("UDP: " + message.Error)
				}
			}
		} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("UDP receive failed: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if time.Now().After(nextRefresh) {
			send("subscribe")
			nextRefresh = time.Now().Add(time.Minute)
		}
	}
}

func (c *Client) JoinRoom(ctx context.Context, room string, events chan<- string) (func(string) error, error) {
	ctx, cancel := context.WithCancel(ctx)
	websocketURL := strings.Replace(c.baseURL, "http://", "ws://", 1)
	websocketURL = strings.Replace(websocketURL, "https://", "wss://", 1)
	token, _ := c.credentials()
	connection, response, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx,
		websocketURL+"/api/ws/rooms/"+url.PathEscape(room), http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		cancel()
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, err
	}
	var writeMu sync.Mutex
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()
	go func() {
		defer cancel()
		defer close(events)
		defer connection.Close()
		for {
			var message struct {
				Username string    `json:"username"`
				Body     string    `json:"body"`
				SentAt   time.Time `json:"sent_at"`
				Type     string    `json:"type"`
				Error    string    `json:"error"`
			}
			if err := connection.ReadJSON(&message); err != nil {
				if ctx.Err() == nil {
					deliverProgress(ctx, events, "Discussion disconnected. Join the room again; chat history is not stored.")
				}
				return
			}
			if message.Type == "message" {
				if !deliverProgress(ctx, events, message.SentAt.Local().Format("02/01/2006 15:04:05")+" "+message.Username+": "+message.Body) {
					return
				}
			} else if message.Type == "error" {
				if !deliverProgress(ctx, events, "Discussion error: "+message.Error) {
					return
				}
			}
		}
	}()
	send := func(body string) error {
		body = strings.TrimSpace(body)
		if len(body) == 0 || len(body) > 2048 {
			return errors.New("message must contain 1-2048 bytes")
		}
		if ctx.Err() != nil {
			return errors.New("discussion disconnected; join the room again")
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return connection.WriteJSON(map[string]string{"body": body})
	}
	return send, nil
}

func (c *Client) request(ctx context.Context, method, path string, body, destination any, authenticated bool) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		token, _ := c.credentials()
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Session-ID", c.sessionID)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, 1<<20)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(limited).Decode(&failure)
		if failure.Error == "" {
			failure.Error = response.Status
		}
		return errors.New(failure.Error)
	}
	if destination != nil {
		return json.NewDecoder(limited).Decode(destination)
	}
	return nil
}

func (c *Client) credentials() (string, User) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token, c.user
}

func writeJSONLine(writer io.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(payload, '\n'))
	return err
}

func randomID(prefix string) string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(value)
}

func nonBlocking(channel chan<- string, value string) {
	select {
	case channel <- value:
	default:
	}
}

func deliverProgress(ctx context.Context, events chan<- string, value string) bool {
	select {
	case events <- value:
		return true
	case <-ctx.Done():
		return false
	}
}
