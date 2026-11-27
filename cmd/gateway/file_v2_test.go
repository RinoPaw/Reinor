package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFileV2BinaryRoundTrip(t *testing.T) {
	h := newHub()
	v, err := newV2Manager(h, "https://example.test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := mcp.NewServer(&mcp.Implementation{Name: "v2-test", Version: "1"}, nil)
	registerV2Tools(m, v)
	token := strings.Repeat("z", 40)
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, token, nil) })
	mux.HandleFunc(v2Prefix, v.serve)
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return m }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http", "ws", 1)+"/agent/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	caps, _ := capability.Encode([]capability.Status{{ID: capability.FileV2, Supported: true, Available: true}})
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "file-v2-test", OS: "linux", Capabilities: caps})
	if err = c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	waitTemporaryConnected(t, h, "file-v2-test")
	got := testJoinMCP(t, server.URL+"/mcp", "download_file", map[string]any{"device_id": "file-v2-test", "path": "/tmp/sample.bin"})
	id, ok := got["transfer_id"].(string)
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
	if f.Type != "file_v2_send" || f.TransferID != id || f.Path != "/tmp/sample.bin" {
		t.Fatal("incorrect dispatch")
	}
	payload := bytes.Repeat([]byte{0, 255, 127, 1}, 2<<20)
	digest := sha256.Sum256(payload)
	send := func(key, hash string) int {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, server.URL+v2Prefix+"receive/"+id, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Reinor-SHA256", hash)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if send("wrong", hex.EncodeToString(digest[:])) != 404 {
		t.Fatal("unauthorized upload accepted")
	}
	if send(f.Feature, hex.EncodeToString(digest[:])) != 204 {
		t.Fatal("binary upload failed")
	}
	got = testJoinMCP(t, server.URL+"/mcp", "get_transfer", map[string]any{"transfer_id": id})
	if got["status"] != "completed" || got["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("result: %v", got)
	}
	link := strings.Replace(got["url"].(string), "https://example.test", server.URL, 1)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	req.Header.Set("Range", "bytes=1-3")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 206 || resp.Header.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(chunk, payload[1:4]) {
		t.Fatalf("range: %d %v", resp.StatusCode, err)
	}
	resp, err = server.Client().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	full, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(full, payload) {
		t.Fatal("corrupt download")
	}
	if !v.cancel(id) {
		t.Fatal("cancel failed")
	}
	resp, err = server.Client().Get(link)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("revoked URL accepted")
	}
}

func TestFileV2UploadRoundTrip(t *testing.T) {
	h := newHub()
	v, err := newV2Manager(h, "https://example.test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("z", 40)
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, token, nil) })
	mux.HandleFunc(v2Prefix, v.serve)
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(server.URL, "http", "ws", 1)+"/agent/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	caps, _ := capability.Encode([]capability.Status{{ID: capability.FileV2, Supported: true, Available: true}})
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "upload-test", OS: "linux", Capabilities: caps})
	if err = c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	waitTemporaryConnected(t, h, "upload-test")
	payload := bytes.Repeat([]byte{0, 255, 127, 1}, 2<<20)
	digest := sha256.Sum256(payload)
	r, err := v.startUpload("upload-test", "/tmp/new.bin", int64(len(payload)), hex.EncodeToString(digest[:]))
	if err != nil {
		t.Fatal(err)
	}
	link := strings.Replace(r.URL, "https://example.test", server.URL, 1)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, link, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("stage: %d", resp.StatusCode)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := wire.Unmarshal(data)
	if err != nil || frame.Type != "file_v2_receive" || frame.SHA256 != hex.EncodeToString(digest[:]) || frame.Size != uint64(len(payload)) {
		t.Fatalf("dispatch: %+v %v", frame, err)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, server.URL+v2Prefix+"source/"+r.ID, nil)
	req.Header.Set("Authorization", "Bearer "+frame.Feature)
	resp, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	full, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || !bytes.Equal(full, payload) {
		t.Fatal("upload source corrupted")
	}
	reply, _ := wire.Marshal(wire.Envelope{Type: "file_result", TaskID: r.ID})
	if err = c.Write(ctx, websocket.MessageBinary, reply); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		r, _ = v.snapshot(r.ID)
		if r.Status == "completed" {
			return
		}
		if r.Status == "failed" || r.Status == "unknown" {
			t.Fatalf("%+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("upload not completed")
}
