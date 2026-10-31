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

// Old and future protocol revisions are never allowed into the active hub.
func TestRejectsMismatchedAgentProtocol(t *testing.T) {
	h := newHub()
	const token = "protocol-test-token-with-enough-characters"
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", func(w http.ResponseWriter, r *http.Request) {
		h.serveAgent(w, r, token, nil)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/agent/ws"
	for _, version := range []uint32{0, 1, wire.ProtocolVersion + 1} {
		t.Run("version-"+string(rune('0'+version)), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{
				"Authorization": []string{"Bearer " + token},
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			hello, _ := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: "incompatible-agent", OS: "windows", ProtocolVersion: version})
			if err := conn.Write(ctx, websocket.MessageBinary, hello); err != nil {
				t.Fatal(err)
			}
			_, _, err = conn.Read(ctx)
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("want policy violation for v%d, got %v", version, err)
			}
			if len(h.list()) != 0 {
				t.Fatalf("rejected v%d must not appear online", version)
			}
		})
	}
}
