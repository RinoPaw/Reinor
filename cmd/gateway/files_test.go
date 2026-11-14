package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func TestFileRequestRequiresCapability(t *testing.T) {
	h := newHub()
	h.agents["old-agent"] = &agent{id: "old-agent", os: "windows"}
	_, err := h.fileRequest(context.Background(), "old-agent", wire.Envelope{Type: "file_info", Path: "C:\\data"})
	if err == nil || !strings.Contains(err.Error(), "file-v1") {
		t.Fatalf("expected capability error, got %v", err)
	}
}

func TestGatewayFileRequestRoundTrip(t *testing.T) {
	h := newHub()
	const token = "fixture-agent-token-with-sufficient-entropy"
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, token, nil) })
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/agent/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "test-files", OS: "windows", Feature: "file-v1", Capabilities: []byte("[]")})
	if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if len(h.list()) == 1 {
			break
		}
		time.Sleep(time.Millisecond * 10)
	}
	if len(h.list()) != 1 || h.list()[0]["file_transfer"] != true {
		t.Fatalf("file capability absent: %+v", h.list())
	}
	response := make(chan wire.Envelope, 1)
	errorCh := make(chan error, 1)
	go func() {
		r, err := h.fileRequest(ctx, "test-files", wire.Envelope{Type: "file_read", Path: "C:\\x.bin", Offset: 123})
		if err != nil {
			errorCh <- err
			return
		}
		response <- r
	}()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("expected binary request, got %d", typ)
	}
	req, err := wire.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if req.Type != "file_read" || req.TaskID == "" || req.Offset != 123 {
		t.Fatalf("bad file request: %+v", req)
	}
	ack, _ := wire.Marshal(wire.Envelope{Type: "file_result", TaskID: req.TaskID, Data: []byte{0, 255, 1}, Offset: 123, Size: 200})
	if err := conn.Write(ctx, websocket.MessageBinary, ack); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-response:
		if r.Size != 200 || len(r.Data) != 3 || r.Data[1] != 255 {
			t.Fatalf("unexpected result: %+v", r)
		}
	case err := <-errorCh:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
