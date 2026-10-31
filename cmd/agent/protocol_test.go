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

func TestAgentRejectsOldGatewayHandshake(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil { return }
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_,data,err := conn.Read(ctx)
		if err != nil { return }
		hello,err := wire.Unmarshal(data)
		if err != nil || hello.ProtocolVersion != wire.ProtocolVersion { return }
		// Simulates a prior Gateway revision responding without a v2 ack.
		ack,_ := wire.Marshal(wire.Envelope{Type:"hello_ack",ProtocolVersion:1})
		_ = conn.Write(ctx, websocket.MessageBinary, ack)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	err := session(ctx, url, "test-agent", "test-session-token")
	if err == nil || !strings.Contains(err.Error(), "incompatible gateway protocol") {
		t.Fatalf("v2 Agent must reject old Gateway: %v", err)
	}
}
