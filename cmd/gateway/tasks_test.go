package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/task"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTaskMCPRoundTrip(t *testing.T) {
	h := newHub()
	store, err := task.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	registerTaskTools(m, h, store)
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
	got := testJoinMCP(t, server.URL+"/mcp", "submit_task", map[string]any{"device_id": "task-test", "command": "echo hello", "timeout_seconds": 300})
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
	reply, _ := wire.Marshal(wire.Envelope{Type: "result", TaskID: id, Stdout: []byte("hello")})
	if err = c.Write(ctx, websocket.MessageBinary, reply); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		r, _ := store.Get(id)
		if !r.Status.Active() {
			if r.Status != task.Succeeded || r.Stdout != "hello" {
				t.Fatalf("result: %+v", r)
			}
			out := testJoinMCP(t, server.URL+"/mcp", "get_task", map[string]any{"task_id": id})
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
