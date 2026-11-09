package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/capability"
	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func dialTemporaryTestAgent(t *testing.T, url, device, secret string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(url, "http://", "ws://", 1), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + secret}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := capability.Encode([]capability.Status{{ID: capability.DesktopCapture, Supported: false, Available: false, Reason: capability.Unsupported}})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: device, OS: "windows", Capabilities: payload})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, msg); err != nil {
		t.Fatal(err)
	}
	return c
}

func waitTemporaryConnected(t *testing.T, h *hub, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		a := h.agents[id]
		h.mu.Unlock()
		if a != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("temporary Agent never registered")
}

func TestTemporaryWebSocketRevocationAndOneUse(t *testing.T) {
	store, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec, secret, err := store.submitTemporary("temp-integration", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.decide(rec.ID, true); err != nil {
		t.Fatal(err)
	}
	h := newHub()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, strings.Repeat("z", 40), store) }))
	defer ts.Close()
	c := dialTemporaryTestAgent(t, ts.URL, rec.DeviceID, secret)
	defer c.CloseNow()
	waitTemporaryConnected(t, h, rec.DeviceID)
	if _, err := store.revokeTemporary(rec.ID); err != nil {
		t.Fatal(err)
	}
	h.dropTemporary(rec.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("revoked agent connection stayed open")
	}
	h.mu.Lock()
	active := h.agents[rec.DeviceID]
	h.mu.Unlock()
	if active != nil {
		t.Fatal("revoked Agent still in hub")
	}
	second := dialTemporaryTestAgent(t, ts.URL, rec.DeviceID, secret)
	defer second.CloseNow()
	if _, _, err := second.Read(ctx); err == nil {
		t.Fatal("one-use ticket reconnected")
	}
}

func TestTemporaryWebSocketExpires(t *testing.T) {
	store, err := newJoinStore(filepath.Join(t.TempDir(), "joins.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec, secret, err := store.submitTemporary("temp-expire", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.decide(rec.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.apply(rec.ID, func(r *joinRecord) error { r.ExpiresAt = time.Now().Add(250 * time.Millisecond); return nil }); err != nil {
		t.Fatal(err)
	}
	h := newHub()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, strings.Repeat("z", 40), store) }))
	defer ts.Close()
	c := dialTemporaryTestAgent(t, ts.URL, rec.DeviceID, secret)
	defer c.CloseNow()
	waitTemporaryConnected(t, h, rec.DeviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, _, err := c.Read(ctx); err == nil {
		t.Fatal("expired session still connected")
	}
	if _, err := store.claim(rec.DeviceID, secret); err == nil {
		t.Fatal("expired credential reused")
	}
}
