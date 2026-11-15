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

func (c *Client) RunProgressSync(ctx context.Context, events chan<- string, status chan<- string) {
	connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", c.tcpAddr)
	if err != nil {
		nonBlocking(status, "TCP offline: "+err.Error())
		return
	}
	defer connection.Close()
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
	nonBlocking(status, "TCP progress sync connected")
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
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()
	for reader.Scan() {
		var message struct {
			Type        string `json:"type"`
			ChallengeID string `json:"challenge_id"`
		}
		if json.Unmarshal(reader.Bytes(), &message) == nil && message.Type == "progress" {
			nonBlocking(events, "Progress synchronized: "+message.ChallengeID)
		}
	}
}

func (c *Client) RunNotifications(ctx context.Context, events chan<- string, status chan<- string) {
	server, err := net.ResolveUDPAddr("udp", c.udpAddr)
	if err != nil {
		nonBlocking(status, "UDP address invalid")
		return
	}
	connection, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		nonBlocking(status, "UDP unavailable: "+err.Error())
		return
	}
	defer connection.Close()
	token, _ := c.credentials()
	clientID := randomID("client-")
	send := func(kind string) {
		payload, _ := json.Marshal(map[string]string{"type": kind, "token": token, "client_id": clientID})
		_, _ = connection.WriteToUDP(payload, server)
	}
	send("subscribe")
	defer send("unsubscribe")
	nonBlocking(status, "UDP notifications subscribed")
	nextRefresh := time.Now().Add(time.Minute)
	buffer := make([]byte, 8*1024)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		count, _, err := connection.ReadFromUDP(buffer)
		if err == nil {
			var message struct {
				Type  string `json:"type"`
				Title string `json:"title"`
			}
			if json.Unmarshal(buffer[:count], &message) == nil && message.Type == "challenge_published" {
				nonBlocking(events, "New challenge: "+message.Title)
			}
		} else if networkError, ok := err.(net.Error); !ok || !networkError.Timeout() {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
		if time.Now().After(nextRefresh) {
			send("subscribe")
			nextRefresh = time.Now().Add(time.Minute)
		}
	}
}

func (c *Client) JoinRoom(ctx context.Context, room string, events chan<- string) (func(string) error, error) {
	websocketURL := strings.Replace(c.baseURL, "http://", "ws://", 1)
	websocketURL = strings.Replace(websocketURL, "https://", "wss://", 1)
	token, _ := c.credentials()
	connection, response, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).DialContext(ctx,
		websocketURL+"/api/ws/rooms/"+url.PathEscape(room), http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
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
		defer close(events)
		defer connection.Close()
		for {
			var message struct {
				Username string `json:"username"`
				Body     string `json:"body"`
			}
			if err := connection.ReadJSON(&message); err != nil {
				return
			}
			nonBlocking(events, message.Username+": "+message.Body)
		}
	}()
	send := func(body string) error {
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
