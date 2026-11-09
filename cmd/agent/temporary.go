package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/coder/websocket"
)

// joinTemporary never reads or writes a persistent enrollment file.
// The Gateway grants a single connection; the Agent makes no retry after it ends.
func joinTemporary(ws, device string) error {
	if !validLocalDeviceID(device) || !strings.HasPrefix(device, "temp-") {
		return errors.New("temporary device ID must begin with temp-")
	}
	ctx := context.Background()
	header := http.Header{"Authorization": []string{"Bearer join-request"}}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, _, err := websocket.Dial(dialCtx, ws, &websocket.DialOptions{HTTPHeader: header})
	cancel()
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(wire.MaxFrame)
	hello, err := wire.Marshal(wire.Envelope{Type: "hello", DeviceID: device, OS: runtime.GOOS, Command: "temporary"})
	if err != nil {
		return err
	}
	if err := c.Write(ctx, websocket.MessageBinary, hello); err != nil {
		return err
	}
	var requestID, ticket string
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return fmt.Errorf("temporary join ended: %w", err)
		}
		if typ != websocket.MessageBinary {
			continue
		}
		msg, err := wire.Unmarshal(data)
		if err != nil {
			return err
		}
		switch msg.Type {
		case "join_pending":
			if msg.TaskID == "" || !strings.HasPrefix(msg.Command, "join_secret_") {
				return errors.New("invalid temporary join response")
			}
			requestID, ticket = msg.TaskID, msg.Command
			fmt.Printf("Temporary support request: %s\nDevice: %s\nWaiting for owner approval (10-minute deadline).\n", requestID, device)
		case "join_approved":
			if requestID == "" || msg.TaskID != requestID || ticket != msg.Command {
				return errors.New("temporary approval does not match request")
			}
			_ = c.Close(websocket.StatusNormalClosure, "enrollment approved")
			fmt.Printf("Temporary access approved. Maximum duration: 60 minutes.\nClose this terminal at any time to stop the Agent.\n")
			return session(ctx, ws, device, ticket)
		case "join_error":
			if msg.Error == "" {
				return errors.New("temporary join rejected")
			}
			return fmt.Errorf("temporary join denied: %s", msg.Error)
		case "join_waiting":
			// Gateway keepalive while the owner reviews the request.
		}
	}
}
