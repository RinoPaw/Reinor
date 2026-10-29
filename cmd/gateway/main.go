package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RinoPaw/Hexveil/internal/oauthlocal"
	"github.com/RinoPaw/Hexveil/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"path/filepath"
)

var validDeviceID = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

func loadSecret(env string) string {
	path := os.Getenv(env)
	if path == "" {
		log.Fatalf("%s required", env)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("read %s: %v", env, err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 {
		log.Fatalf("%s requires at least 32 characters", env)
	}
	return token
}
func bearer(r *http.Request, token string) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(token) && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

type agent struct {
	id, os    string
	conn      *websocket.Conn
	writeMu   sync.Mutex
	busyMu    sync.Mutex
	busy      bool
	connected time.Time
}

func (a *agent) send(ctx context.Context, frame wire.Envelope) error {
	b, err := wire.Marshal(frame)
	if err != nil {
		return err
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.conn.Write(ctx, websocket.MessageBinary, b)
}
func (a *agent) reserve() bool {
	a.busyMu.Lock()
	defer a.busyMu.Unlock()
	if a.busy {
		return false
	}
	a.busy = true
	return true
}
func (a *agent) release() { a.busyMu.Lock(); a.busy = false; a.busyMu.Unlock() }

type pending struct {
	agent *agent
	ch    chan wire.Envelope
}
type hub struct {
	mu      sync.Mutex
	agents  map[string]*agent
	pending map[string]pending
	known   map[string]string
}

func newHub() *hub {
	return &hub{agents: make(map[string]*agent), pending: make(map[string]pending), known: make(map[string]string)}
}
func (h *hub) register(a *agent) {
	h.mu.Lock()
	old := h.agents[a.id]
	h.agents[a.id] = a
	h.known[a.id] = a.os
	h.mu.Unlock()
	if old != nil {
		_ = old.conn.Close(websocket.StatusPolicyViolation, "device reconnected")
	}
	log.Printf("agent online id=%s os=%s", a.id, a.os)
}
func (h *hub) remove(a *agent) {
	h.mu.Lock()
	if h.agents[a.id] == a {
		delete(h.agents, a.id)
	}
	for id, p := range h.pending {
		if p.agent == a {
			select {
			case p.ch <- wire.Envelope{Type: "result", TaskID: id, Error: "connection lost; execution status unknown"}:
			default:
			}
		}
	}
	h.mu.Unlock()
	log.Printf("agent disconnected id=%s", a.id)
}
func (h *hub) list() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	arr := make([]map[string]any, 0, len(h.known))
	for id, osName := range h.known {
		item := map[string]any{"device_id": id, "os": osName, "online": false}
		if a := h.agents[id]; a != nil {
			item["online"] = true
			item["connected_at"] = a.connected.UTC().Format(time.RFC3339)
		}
		arr = append(arr, item)
	}
	return arr
}
func (h *hub) completed(a *agent, v wire.Envelope) {
	h.mu.Lock()
	p, ok := h.pending[v.TaskID]
	h.mu.Unlock()
	if ok && p.agent == a {
		select {
		case p.ch <- v:
		default:
		}
	}
}
func (h *hub) execute(ctx context.Context, deviceID, cmd string, timeout time.Duration) (wire.Envelope, error) {
	if !validDeviceID.MatchString(deviceID) || len(cmd) == 0 || len(cmd) > 4096 {
		return wire.Envelope{}, errors.New("invalid device_id or command")
	}
	if timeout <= 0 || timeout > 20*time.Second {
		timeout = 20 * time.Second
	}
	h.mu.Lock()
	a := h.agents[deviceID]
	h.mu.Unlock()
	if a == nil {
		return wire.Envelope{}, errors.New("device offline")
	}
	if !a.reserve() {
		return wire.Envelope{}, errors.New("device busy")
	}
	defer a.release()
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return wire.Envelope{}, err
	}
	id := hex.EncodeToString(buf)
	response := make(chan wire.Envelope, 1)
	h.mu.Lock()
	h.pending[id] = pending{agent: a, ch: response}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }()
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := a.send(writeCtx, wire.Envelope{Type: "execute", TaskID: id, Command: cmd, TimeoutMS: uint32(timeout.Milliseconds())})
	cancel()
	if err != nil {
		return wire.Envelope{}, fmt.Errorf("dispatch failed; status unknown: %w", err)
	}
	timer := time.NewTimer(timeout + 5*time.Second)
	defer timer.Stop()
	select {
	case r := <-response:
		return r, nil
	case <-timer.C:
		return wire.Envelope{}, errors.New("execution timed out; status unknown")
	case <-ctx.Done():
		return wire.Envelope{}, fmt.Errorf("request canceled; execution status unknown: %w", ctx.Err())
	}
}
func (h *hub) serveAgent(w http.ResponseWriter, r *http.Request, token string, joins *joinStore) {
	if r.Header.Get("Authorization") == "Bearer join-request" {
		h.serveJoin(w, r, joins)
		return
	}
	legacy := bearer(r, token)
	incoming := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	ticketMode := strings.HasPrefix(incoming, "join_secret_")
	if !legacy && !ticketMode {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("ws accept: %v", err)
		return
	}
	c.SetReadLimit(wire.MaxFrame)
	ctx := r.Context()
	helloCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	typ, data, err := c.Read(helloCtx)
	cancel()
	if err != nil || typ != websocket.MessageBinary {
		_ = c.Close(websocket.StatusPolicyViolation, "hello required")
		return
	}
	hello, err := wire.Unmarshal(data)
	if err != nil || hello.Type != "hello" || !validDeviceID.MatchString(hello.DeviceID) {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	if !legacy && (joins == nil || !joins.allowed(hello.DeviceID, incoming)) {
		_ = c.Close(websocket.StatusPolicyViolation, "device not approved")
		return
	}
	a := &agent{id: hello.DeviceID, os: hello.OS, conn: c, connected: time.Now()}
	h.register(a)
	defer func() { h.remove(a); _ = c.CloseNow() }()
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				pctx, stop := context.WithTimeout(ctx, 10*time.Second)
				err := c.Ping(pctx)
				stop()
				if err != nil {
					_ = c.CloseNow()
					return
				}
			}
		}
	}()
	for {
		typ, data, err = c.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		frame, err := wire.Unmarshal(data)
		if err != nil {
			return
		}
		if frame.Type == "result" {
			h.completed(a, frame)
		}
	}
}

type noArgs struct{}
type execArgs struct {
	DeviceID string `json:"device_id" jsonschema:"Device ID from list_devices"`
	Command  string `json:"command" jsonschema:"PowerShell command to execute"`
}

func result(content any, isError bool) *mcp.CallToolResult {
	b, _ := json.Marshal(content)
	return &mcp.CallToolResult{IsError: isError, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
func main() {
	agentToken := loadSecret("RINOMESH_AGENT_TOKEN_FILE")
	controlToken := loadSecret("RINOMESH_CONTROL_TOKEN_FILE")
	h := newHub()
	m := mcp.NewServer(&mcp.Implementation{Name: "rinomesh-control", Version: "0.1.0"}, nil)
	mcp.AddTool(m, &mcp.Tool{Name: "list_devices", Description: "List online RinoMesh devices."}, func(ctx context.Context, req *mcp.CallToolRequest, args noArgs) (*mcp.CallToolResult, any, error) {
		return result(h.list(), false), nil, nil
	})
	mcp.AddTool(m, &mcp.Tool{Name: "execute_command", Description: "Run a PowerShell command on an online device. May change files or system state."}, func(ctx context.Context, req *mcp.CallToolRequest, args execArgs) (*mcp.CallToolResult, any, error) {
		r, err := h.execute(ctx, args.DeviceID, args.Command, 20*time.Second)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(map[string]any{"task_id": r.TaskID, "device_id": args.DeviceID, "exit_code": r.ExitCode, "stdout": string(r.Stdout), "stderr": string(r.Stderr), "error": r.Error}, r.Error != ""), nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return m }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	joinDB := os.Getenv("RINOMESH_JOIN_DB")
	if joinDB == "" {
		joinDB = "/tmp/rinomesh-joins.json"
	}
	joins, err := newJoinStore(joinDB)
	if err != nil {
		log.Fatalf("open join registry: %v", err)
	}
	login, err := oauthlocal.New(controlToken, filepath.Join(filepath.Dir(joinDB), "oauth-clients.json"))
	if err != nil {
		log.Fatalf("open local OAuth: %v", err)
	}
	registerJoinTools(m, joins)
	mux := http.NewServeMux()
	// Only /mcp is exposed externally. It accepts OAuth access tokens and
	// preserves the existing owner bearer token for legacy local integrations.
	login.InstallRoutes(mux)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearerValue := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearer(r, controlToken) && !login.ValidAccess(bearerValue) {
			login.Challenge(w)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	joinRoutes(mux, joins)
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, agentToken, joins) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method not allowed", 405)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	listen := os.Getenv("RINOMESH_LISTEN_ADDR")
	if listen == "" {
		listen = "127.0.0.1:18889"
	}
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second}
	log.Printf("rinomesh gateway listening %s", listen)
	log.Fatal(server.ListenAndServe())
}
