package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testJoinRequest(t *testing.T, endpoint string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var output map[string]any
	_ = json.NewDecoder(response.Body).Decode(&output)
	return response.StatusCode, output
}
func testJoinMCP(t *testing.T, endpoint, name string, args any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	request, _ := http.NewRequest("POST", endpoint, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json,text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("RPC status %d", response.StatusCode)
	}
	var out struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Content) != 1 {
		t.Fatal("unexpected MCP response")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out.Result.Content[0].Text), &result); err != nil {
		t.Fatalf("bad MCP result: %v", err)
	}
	return result
}
func TestSingleGatewayJoinToExecute(t *testing.T) {
	store, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHub()
	control := mcp.NewServer(&mcp.Implementation{Name: "test-control", Version: "1"}, nil)
	registerJoinTools(control, store)
	mux := http.NewServeMux()
	joinRoutes(mux, store)
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return control }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) {
		h.serveAgent(w, r, "legacy-agent-token-long-enough", store)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	code, request := testJoinRequest(t, server.URL+"/agent/join", map[string]string{"device_id": "test-device-joined", "os": "windows"})
	if code != 201 {
		t.Fatalf("join request status: %d %v", code, request)
	}
	id := request["request_id"].(string)
	ticket := request["ticket"].(string)
	code, status := testJoinRequest(t, server.URL+"/agent/join/status", map[string]string{"request_id": id, "ticket": ticket})
	if code != 200 || status["state"] != "pending" {
		t.Fatalf("expected pending: %#v", status)
	}
	if store.allowed("test-device-joined", ticket) {
		t.Fatal("pending device can connect")
	}
	response := testJoinMCP(t, server.URL+"/mcp", "approve_join_request", map[string]string{"request_id": id})
	if response["state"] != "approved" {
		t.Fatalf("MCP approve failed: %v", response)
	}
	code, status = testJoinRequest(t, server.URL+"/agent/join/status", map[string]string{"request_id": id, "ticket": ticket})
	if code != 200 || status["state"] != "approved" {
		t.Fatalf("approval invisible: %#v", status)
	}
	if store.allowed("different-device", ticket) {
		t.Fatal("ticket not device-bound")
	}
	restored, err := newJoinStore(store.file)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.allowed("test-device-joined", ticket) {
		t.Fatal("approval not persistent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/agent/ws"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + ticket}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "test-device-joined", OS: "windows", Capabilities: []byte(`[{"id":"desktop.capture","supported":true,"available":true}]`)})
	if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if len(h.list()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(h.list()) != 1 {
		t.Fatal("approved device did not join hub")
	}
	result := make(chan wire.Envelope, 1)
	errs := make(chan error, 1)
	go func() {
		reply, err := h.execute(ctx, "test-device-joined", "hostname", 5*time.Second)
		if err != nil {
			errs <- err
			return
		}
		result <- reply
	}()
	_, frame, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	command, err := wire.Unmarshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	if command.Command != "hostname" || command.TaskID == "" {
		t.Fatalf("bad command: %+v", command)
	}
	answer, _ := wire.Marshal(wire.Envelope{Type: "result", TaskID: command.TaskID, Stdout: []byte("test-device-joined\n"), ExitCode: 0})
	if err := conn.Write(ctx, websocket.MessageBinary, answer); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-result:
		if string(r.Stdout) != "test-device-joined\n" || r.ExitCode != 0 {
			t.Fatalf("bad result: %+v", r)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("timeout")
	}
}

func TestJoinThroughExistingWebSocketEndpoint(t *testing.T) {
	store, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHub()
	control := mcp.NewServer(&mcp.Implementation{Name: "test-control", Version: "1"}, nil)
	registerJoinTools(control, store)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return control }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) {
		h.serveAgent(w, r, "legacy-secret-over-32-characters", store)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	socketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/agent/ws"
	conn, _, err := websocket.Dial(ctx, socketURL, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer join-request"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "pc-joined", OS: "windows", Capabilities: []byte(`[{"id":"desktop.capture","supported":true,"available":true}]`)})
	if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := wire.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != "join_pending" || first.TaskID == "" {
		t.Fatalf("join not pending: %+v", first)
	}
	if len(h.list()) != 0 {
		t.Fatal("pending device already online")
	}
	for i := 0; i < 2; i++ {
		_, heartbeatData, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("join connection closed before approval: %v", err)
		}
		frame, err := wire.Unmarshal(heartbeatData)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Type != "join_waiting" || frame.TaskID != first.TaskID {
			t.Fatalf("bad heartbeat %+v", frame)
		}
	}
	approved := testJoinMCP(t, server.URL+"/mcp", "approve_join_request", map[string]string{"request_id": first.TaskID})
	if approved["state"] != "approved" {
		t.Fatal("approval unsuccessful")
	}
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := wire.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if second.Type != "join_approved" || !strings.HasPrefix(second.Command, "join_secret_") {
		t.Fatalf("join not approved: %+v", second)
	}
	if !store.allowed("pc-joined", second.Command) {
		t.Fatal("approval ticket invalid")
	}
	if store.allowed("different-pc", second.Command) {
		t.Fatal("ticket accepted for different device")
	}
}

func TestApprovedDeviceReEnrollmentAndRecovery(t *testing.T) {
	store, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, oldTicket, err := store.submit("rejoin-pc", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.decide(first.ID, true); err != nil {
		t.Fatal(err)
	}
	second, newTicket, err := store.submit("rejoin-pc", "windows")
	if err != nil {
		t.Fatalf("approved device cannot re-enroll after losing ticket: %v", err)
	}
	if !store.allowed("rejoin-pc", oldTicket) {
		t.Fatal("previous approval revoked before owner approved replacement")
	}
	if _, err := store.resume("rejoin-pc", newTicket); err != nil {
		t.Fatal(err)
	}
	if _, err := store.resume("wrong-pc", newTicket); err == nil {
		t.Fatal("recovery ticket accepted for another device")
	}
	pending, err := store.resume("rejoin-pc", newTicket)
	if err != nil || pending.State != "pending" {
		t.Fatal("pending recovery lost")
	}
	if _, err := store.decide(second.ID, true); err != nil {
		t.Fatal(err)
	}
	approved, err := store.resume("rejoin-pc", newTicket)
	if err != nil || approved.State != "approved" {
		t.Fatal("approved recovery lost")
	}
	if store.allowed("rejoin-pc", oldTicket) {
		t.Fatal("replaced ticket still valid")
	}
	if !store.allowed("rejoin-pc", newTicket) {
		t.Fatal("new ticket not valid")
	}
	restarted, err := newJoinStore(store.file)
	if err != nil {
		t.Fatal(err)
	}
	if restored, err := restarted.resume("rejoin-pc", newTicket); err != nil || restored.State != "approved" {
		t.Fatal("approved recovery not persistent")
	}
}
