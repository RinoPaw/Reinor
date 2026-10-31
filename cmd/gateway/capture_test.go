package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

func TestGatewayCaptureRoundTrip(t *testing.T) {
	h := newHub()
	const token = "development-agent-token-32-characters-or-more"
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) { h.serveAgent(w, r, token, nil) })
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/agent/ws"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "screen-test", OS: "windows"})
	if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		h.mu.Lock()
		connected := h.agents["screen-test"] != nil
		h.mu.Unlock()
		if connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	done := make(chan wire.Envelope, 1)
	failed := make(chan error, 1)
	go func() {
		got, err := h.capture(ctx, "screen-test", 1280, 65, 3*time.Second)
		if err != nil {
			failed <- err
			return
		}
		done <- got
	}()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request, err := wire.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if request.Type != "capture_screen" || request.Width != 1280 || request.Quality != 65 || request.TaskID == "" {
		t.Fatalf("unexpected capture request: %#v", request)
	}
	jpg := []byte{0xff, 0xd8, 0xff, 0xd9}
	response, _ := wire.Marshal(wire.Envelope{Type: "capture_result", TaskID: request.TaskID, Data: jpg, MIMEType: "image/jpeg", Width: 1280, Height: 800, OriginX: -500, OriginY: 0, PhysicalWidth: 2560, PhysicalHeight: 1600})
	if err := conn.Write(ctx, websocket.MessageBinary, response); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if !bytes.Equal(got.Data, jpg) || got.Width != 1280 || got.OriginX != -500 || got.PhysicalWidth != 2560 {
			t.Fatalf("mismatched screenshot: %#v", got)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCaptureValidation(t *testing.T) {
	h := newHub()
	for _, tc := range []struct{ width, quality uint32 }{
		{319, 75}, {3841, 75}, {1600, 34}, {1600, 91},
	} {
		if _, err := h.capture(context.Background(), "test", tc.width, tc.quality, time.Second); err == nil {
			t.Fatalf("accepted invalid capture dimensions: %+v", tc)
		}
	}
}

func TestCaptureRejectsNonWindowsAgent(t *testing.T) {
	h := newHub()
	h.agents["linux"] = &agent{id: "linux", os: "linux"}
	_, err := h.capture(context.Background(), "linux", 0, 0, time.Second)
	if err == nil || !strings.Contains(err.Error(), "desktop capture requires Windows") {
		t.Fatalf("expected unsupported OS: %v", err)
	}
}
