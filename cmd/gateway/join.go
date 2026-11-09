package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"context"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
)

type joinRecord struct {
	ID          string    `json:"request_id"`
	DeviceID    string    `json:"device_id"`
	OS          string    `json:"os"`
	State       string    `json:"state"`
	TicketHash  string    `json:"ticket_hash"`
	RequestedAt time.Time `json:"requested_at"`
	Mode        string    `json:"mode,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	UsedAt      time.Time `json:"used_at,omitempty"`
}

const (
	temporaryPendingTTL = 10 * time.Minute
	temporarySessionTTL = 60 * time.Minute
)

type joinStore struct {
	mu      sync.Mutex
	file    string
	records map[string]joinRecord
}

func newJoinStore(file string) (*joinStore, error) {
	if file == "" {
		return nil, errors.New("join storage path required")
	}
	s := &joinStore{file: file, records: map[string]joinRecord{}}
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.records); err != nil {
		return nil, err
	}
	if s.records == nil {
		return nil, errors.New("join database corrupted")
	}
	return s, nil
}
func joinRandom(prefix string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
func ticketHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func equalHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *joinStore) save(next map[string]joinRecord) error {
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.file), ".join-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), s.file); err != nil {
		return err
	}
	s.records = next
	return nil
}
func (s *joinStore) apply(id string, fn func(*joinRecord) error) (joinRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.records[id]
	if !ok {
		return joinRecord{}, errors.New("join request not found")
	}
	if err := fn(&current); err != nil {
		return joinRecord{}, err
	}
	next := make(map[string]joinRecord, len(s.records))
	for k, v := range s.records {
		next[k] = v
	}
	next[id] = current
	if err := s.save(next); err != nil {
		return joinRecord{}, err
	}
	current.TicketHash = ""
	return current, nil
}
func (s *joinStore) submit(device, osName string) (joinRecord, string, error) {
	if !validDeviceID.MatchString(device) || len(osName) > 64 {
		return joinRecord{}, "", errors.New("invalid device details")
	}
	id, err := joinRandom("join_")
	if err != nil {
		return joinRecord{}, "", err
	}
	ticket, err := joinRandom("join_secret_")
	if err != nil {
		return joinRecord{}, "", err
	}
	rec := joinRecord{ID: id, DeviceID: device, OS: osName, State: "pending", TicketHash: ticketHash(ticket), RequestedAt: time.Now().UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-enrollment is permitted; an existing approved ticket remains valid until
	// the owner explicitly approves the replacement request.
	next := make(map[string]joinRecord, len(s.records)+1)
	for k, v := range s.records {
		if v.DeviceID == device && v.State == "pending" {
			v.State = "superseded"
		}
		next[k] = v
	}
	next[id] = rec
	if err := s.save(next); err != nil {
		return joinRecord{}, "", err
	}
	rec.TicketHash = ""
	return rec, ticket, nil
}

func (s *joinStore) submitTemporary(device, osName string) (joinRecord, string, error) {
	if !validDeviceID.MatchString(device) || !strings.HasPrefix(device, "temp-") || len(osName) > 64 {
		return joinRecord{}, "", errors.New("temporary device ID must begin with temp-")
	}
	id, err := joinRandom("join_")
	if err != nil {
		return joinRecord{}, "", err
	}
	ticket, err := joinRandom("join_secret_")
	if err != nil {
		return joinRecord{}, "", err
	}
	now := time.Now().UTC()
	rec := joinRecord{ID: id, DeviceID: device, OS: osName, State: "pending", Mode: "temporary", TicketHash: ticketHash(ticket), RequestedAt: now, ExpiresAt: now.Add(temporaryPendingTTL)}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, prior := range s.records {
		if prior.DeviceID == device && (prior.State == "pending" && (prior.Mode != "temporary" || now.Before(prior.ExpiresAt)) || prior.State == "approved" && (prior.Mode != "temporary" || now.Before(prior.ExpiresAt))) {
			return joinRecord{}, "", errors.New("temporary device ID already in use")
		}
	}
	next := make(map[string]joinRecord, len(s.records)+1)
	for k, v := range s.records {
		next[k] = v
	}
	next[id] = rec
	if err := s.save(next); err != nil {
		return joinRecord{}, "", err
	}
	rec.TicketHash = ""
	return rec, ticket, nil
}

func (s *joinStore) revokeTemporary(id string) (joinRecord, error) {
	return s.apply(id, func(rec *joinRecord) error {
		if rec.Mode != "temporary" {
			return errors.New("request is not temporary")
		}
		if rec.State != "revoked" {
			rec.State = "revoked"
		}
		return nil
	})
}

func (s *joinStore) claim(device, ticket string) (joinRecord, error) {
	if !validDeviceID.MatchString(device) || !strings.HasPrefix(ticket, "join_secret_") {
		return joinRecord{}, errors.New("invalid credential")
	}
	digest := ticketHash(ticket)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, rec := range s.records {
		if rec.DeviceID != device || rec.State != "approved" || !equalHash(rec.TicketHash, digest) {
			continue
		}
		if rec.Mode == "temporary" {
			if !now.Before(rec.ExpiresAt) || !rec.UsedAt.IsZero() {
				return joinRecord{}, errors.New("temporary session expired or already consumed")
			}
			rec.UsedAt = now
			next := make(map[string]joinRecord, len(s.records))
			for k, v := range s.records {
				next[k] = v
			}
			next[id] = rec
			if err := s.save(next); err != nil {
				return joinRecord{}, err
			}
		}
		rec.TicketHash = ""
		return rec, nil
	}
	return joinRecord{}, errors.New("credential not approved")
}

func (s *joinStore) list() []joinRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []joinRecord{}
	for _, r := range s.records {
		r.TicketHash = ""
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RequestedAt.Before(out[j].RequestedAt) })
	return out
}
func (s *joinStore) decide(id string, approve bool) (joinRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.records[id]
	if !ok {
		return joinRecord{}, errors.New("join request not found")
	}
	if current.State != "pending" {
		return joinRecord{}, errors.New("request already decided")
	}
	if current.Mode == "temporary" && !time.Now().Before(current.ExpiresAt) {
		return joinRecord{}, errors.New("temporary join request expired")
	}
	next := make(map[string]joinRecord, len(s.records))
	for k, v := range s.records {
		if approve && k != id && v.DeviceID == current.DeviceID && v.State == "approved" {
			v.State = "superseded"
		}
		next[k] = v
	}
	if approve {
		current.State = "approved"
		if current.Mode == "temporary" {
			current.ExpiresAt = time.Now().UTC().Add(temporarySessionTTL)
		}
	} else {
		current.State = "rejected"
	}
	next[id] = current
	if err := s.save(next); err != nil {
		return joinRecord{}, err
	}
	current.TicketHash = ""
	return current, nil
}
func (s *joinStore) status(id, ticket string) (joinRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok || len(ticket) < 32 || !equalHash(rec.TicketHash, ticketHash(ticket)) {
		return joinRecord{}, errors.New("unknown request or ticket")
	}
	if rec.Mode == "temporary" && !time.Now().Before(rec.ExpiresAt) {
		rec.State = "expired"
	}
	rec.TicketHash = ""
	return rec, nil
}
func (s *joinStore) resume(device, ticket string) (joinRecord, error) {
	if !validDeviceID.MatchString(device) || !strings.HasPrefix(ticket, "join_secret_") {
		return joinRecord{}, errors.New("invalid join recovery credential")
	}
	digest := ticketHash(ticket)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		if rec.DeviceID == device && rec.Mode != "temporary" && equalHash(rec.TicketHash, digest) && (rec.State == "pending" || rec.State == "approved") {
			rec.TicketHash = ""
			return rec, nil
		}
	}
	return joinRecord{}, errors.New("join recovery credential not found")
}
func (s *joinStore) allowed(device, ticket string) bool {
	if len(ticket) < 32 {
		return false
	}
	digest := ticketHash(ticket)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.records {
		if r.DeviceID == device && r.State == "approved" && (r.Mode != "temporary" || (time.Now().Before(r.ExpiresAt) && r.UsedAt.IsZero())) && equalHash(r.TicketHash, digest) {
			return true
		}
	}
	return false
}
func handleJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func joinRoutes(mux *http.ServeMux, s *joinStore) {
	mux.HandleFunc("/agent/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		var args struct {
			DeviceID string `json:"device_id"`
			OS       string `json:"os"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&args); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		rec, ticket, err := s.submit(args.DeviceID, args.OS)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		handleJSON(w, map[string]any{"request_id": rec.ID, "device_id": rec.DeviceID, "state": rec.State, "ticket": ticket}, 201)
	})
	mux.HandleFunc("/agent/join/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		var args struct {
			RequestID string `json:"request_id"`
			Ticket    string `json:"ticket"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&args); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		rec, err := s.status(args.RequestID, args.Ticket)
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		handleJSON(w, map[string]any{"request_id": rec.ID, "device_id": rec.DeviceID, "state": rec.State}, 200)
	})
}

type joinAction struct {
	RequestID string `json:"request_id" jsonschema:"Join request identifier"`
}

func registerJoinTools(server *mcp.Server, s *joinStore, h *hub) {
	listHandler := func(_ context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		return result(s.list(), false), nil, nil
	}
	mcp.AddTool(server, &mcp.Tool{Name: "list_join_requests", Description: "List pending and decided device connection requests."}, listHandler)
	mcp.AddTool(server, &mcp.Tool{Name: "approve_join_request", Description: "Approve a pending device request. The PC can then connect to Gateway."}, func(_ context.Context, _ *mcp.CallToolRequest, args joinAction) (*mcp.CallToolResult, any, error) {
		rec, err := s.decide(args.RequestID, true)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(map[string]string{"request_id": rec.ID, "device_id": rec.DeviceID, "state": rec.State}, false), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "revoke_temporary_session", Description: "Immediately revoke a temporary support session by request ID, disconnect its Agent, and invalidate its ticket."}, func(_ context.Context, _ *mcp.CallToolRequest, args joinAction) (*mcp.CallToolResult, any, error) {
		rec, err := s.revokeTemporary(args.RequestID)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		h.dropTemporary(rec.ID)
		return result(map[string]any{"request_id": rec.ID, "device_id": rec.DeviceID, "state": rec.State}, false), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "reject_join_request", Description: "Reject a pending device request."}, func(_ context.Context, _ *mcp.CallToolRequest, args joinAction) (*mcp.CallToolResult, any, error) {
		rec, err := s.decide(args.RequestID, false)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(map[string]string{"request_id": rec.ID, "device_id": rec.DeviceID, "state": rec.State}, false), nil, nil
	})
}

func (h *hub) serveJoin(w http.ResponseWriter, r *http.Request, s *joinStore) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("join accept: %v", err)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(wire.MaxFrame)
	ctx := r.Context()
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	typ, frame, err := c.Read(readCtx)
	cancel()
	if err != nil || typ != websocket.MessageBinary {
		return
	}
	hello, err := wire.Unmarshal(frame)
	if err != nil || hello.Type != "hello" || !validDeviceID.MatchString(hello.DeviceID) {
		return
	}
	var rec joinRecord
	var ticket string
	if hello.Command == "temporary" {
		rec, ticket, err = s.submitTemporary(hello.DeviceID, hello.OS)
	} else if hello.Command != "" {
		ticket = hello.Command
		rec, err = s.resume(hello.DeviceID, ticket)
	} else {
		rec, ticket, err = s.submit(hello.DeviceID, hello.OS)
	}
	if err != nil {
		data, _ := wire.Marshal(wire.Envelope{Type: "join_error", Error: err.Error()})
		_ = c.Write(ctx, websocket.MessageBinary, data)
		return
	}
	if rec.State == "approved" {
		approved, _ := wire.Marshal(wire.Envelope{Type: "join_approved", TaskID: rec.ID, Command: ticket})
		_ = c.Write(ctx, websocket.MessageBinary, approved)
		return
	}
	pending, _ := wire.Marshal(wire.Envelope{Type: "join_pending", TaskID: rec.ID, DeviceID: rec.DeviceID, Command: ticket})
	if err := c.Write(ctx, websocket.MessageBinary, pending); err != nil {
		return
	}
	timer := time.NewTicker(3 * time.Second)
	defer timer.Stop()
	heartbeatTicks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			status, err := s.status(rec.ID, ticket)
			if err != nil {
				return
			}
			if status.State == "rejected" || status.State == "superseded" || status.State == "expired" || status.State == "revoked" {
				data, _ := wire.Marshal(wire.Envelope{Type: "join_error", TaskID: rec.ID, Error: "request rejected"})
				_ = c.Write(ctx, websocket.MessageBinary, data)
				return
			}
			if status.State == "approved" {
				data, _ := wire.Marshal(wire.Envelope{Type: "join_approved", TaskID: rec.ID, Command: ticket})
				_ = c.Write(ctx, websocket.MessageBinary, data)
				return
			}
			// A binary heartbeat keeps the Nginx upstream active while waiting.
			// WebSocket Ping would require a concurrent reader to process the Pong.
			heartbeatTicks++
			if heartbeatTicks >= 5 {
				heartbeatTicks = 0
				alive, _ := wire.Marshal(wire.Envelope{Type: "join_waiting", TaskID: rec.ID})
				writeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				err := c.Write(writeCtx, websocket.MessageBinary, alive)
				stop()
				if err != nil {
					return
				}
			}
		}
	}
}
