package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/operation"
	"github.com/RinoPaw/Reinor/internal/task"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTaskMCPRoundTrip(t *testing.T)       { testTaskRoundTrip(t, false) }
func TestTaskCancelMCPRoundTrip(t *testing.T) { testTaskRoundTrip(t, true) }
func testTaskRoundTrip(t *testing.T, cancelTask bool) {
	h := newHub()
	store, err := task.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	reg := operation.New(false, h.operationAvailability)
	registerTaskTools(m, h, store, reg)
	reg.Install(m)
	token := strings.Repeat("z", 40)
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, token, nil) })
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return m }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http", "ws", 1)+"/agent/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	caps, err := capability.Encode([]capability.Status{{ID: capability.AsyncTask, Supported: true, Available: true}})
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "task-test", OS: "linux", Capabilities: caps})
	if err = c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	waitTemporaryConnected(t, h, "task-test")
	submit := reg.Discover(operation.DiscoverArgs{Operation: "task.submit"})[0]
	got := testJoinMCP(t, server.URL+"/mcp", "invoke", map[string]any{"operation": submit.Name, "revision": submit.Revision, "arguments": map[string]any{"device_id": "task-test", "command": "echo hello", "timeout_seconds": 300}})
	id, ok := got["task_id"].(string)
	if !ok {
		t.Fatalf("submit: %v", got)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := wire.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != "execute_task" || f.TimeoutMS != 300000 || f.TaskID != id || f.Command != "echo hello" {
		t.Fatalf("request: %+v", f)
	}
	progress, _ := wire.Marshal(wire.Envelope{Type: "task_progress", TaskID: id, Stdout: []byte("hel")})
	if err = c.Write(ctx, websocket.MessageBinary, progress); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		r, _ := store.Get(id)
		if r.Status == task.Running && r.Stdout == "hel" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ctx.Err() != nil {
		t.Fatal("live progress not persisted")
	}
	expected := task.Succeeded
	replyError := ""
	if cancelTask {
		op := reg.Discover(operation.DiscoverArgs{Operation: "task.cancel"})[0]
		call := map[string]any{"operation": op.Name, "revision": op.Revision, "arguments": map[string]any{"task_id": id}}
		out := testJoinMCP(t, server.URL+"/mcp", "invoke", call)
		if out["cancel_requested"] != true || out["status"] != "running" {
			t.Fatalf("cancel request: %v", out)
		}
		out = testJoinMCP(t, server.URL+"/mcp", "invoke", call)
		if out["cancel_requested"] != true {
			t.Fatalf("repeat cancellation: %v", out)
		}
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := wire.Unmarshal(data)
		if err != nil || frame.Type != "cancel_task" || frame.TaskID != id {
			t.Fatalf("cancel frame: %+v %v", frame, err)
		}
		expected = task.Cancelled
		replyError = "task cancelled"
	}
	reply, _ := wire.Marshal(wire.Envelope{Type: "result", TaskID: id, Stdout: []byte("hello"), Error: replyError})
	if err = c.Write(ctx, websocket.MessageBinary, reply); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		r, _ := store.Get(id)
		if !r.Status.Active() {
			if r.Status != expected || r.Stdout != "hello" || r.CancelRequested != cancelTask {
				t.Fatalf("result: %+v", r)
			}
			get := reg.Discover(operation.DiscoverArgs{Operation: "task.get"})[0]
			out := testJoinMCP(t, server.URL+"/mcp", "query", map[string]any{"operation": get.Name, "revision": get.Revision, "arguments": map[string]any{"task_id": id}})
			if out["stdout"] != "hello" {
				t.Fatalf("get: %v", out)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
}
func TestTaskRejectsOldAgent(t *testing.T) {
	h := newHub()
	h.agents["old"] = &agent{id: "old"}
	s, err := task.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = submitTask(h, s, submitTaskArgs{execArgs: execArgs{DeviceID: "old", Command: "echo hello"}})
	if err == nil {
		t.Fatal("accepted old agent")
	}
	if len(s.List("", 20)) != 0 {
		t.Fatal("recorded rejected task")
	}
}
