package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type v2IDArgs struct {
	ID string `json:"transfer_id"`
}

type v2UploadArgs struct {
	DeviceID string `json:"device_id"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

func registerV2Tools(m *mcp.Server, v *v2Manager) {
	mcp.AddTool(m, &mcp.Tool{Name: "upload_file", Description: "Authorize binary upload to an online permanent file-v2 device. PUT original bytes to the returned HTTPS URL with Content-Type application/octet-stream and the declared size and SHA-256. Never overwrites."}, func(_ context.Context, _ *mcp.CallToolRequest, args v2UploadArgs) (*mcp.CallToolResult, any, error) {
		r, err := v.startUpload(args.DeviceID, args.Path, args.Size, args.SHA256)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(r, false), nil, nil
	})
	mcp.AddTool(m, &mcp.Tool{Name: "download_file", Description: "Transfer a regular binary file from an online permanent file-v2 device to the Gateway. Returns a transfer ID; use get_transfer to retrieve the short-lived download URL. Does not interpret the file."}, func(_ context.Context, _ *mcp.CallToolRequest, args filePathArgs) (*mcp.CallToolResult, any, error) {
		r, err := v.start(args.DeviceID, args.Path)
		if err != nil {
			return result(map[string]string{"error": err.Error()}, true), nil, nil
		}
		return result(r, false), nil, nil
	})
	mcp.AddTool(m, &mcp.Tool{Name: "get_transfer", Description: "Get binary transfer status, staged byte progress, SHA-256 and short-lived URL when available."}, func(_ context.Context, _ *mcp.CallToolRequest, args v2IDArgs) (*mcp.CallToolResult, any, error) {
		r, ok := v.snapshot(args.ID)
		if !ok {
			return result(map[string]string{"error": "transfer not found or expired"}, true), nil, nil
		}
		return result(r, false), nil, nil
	})
	mcp.AddTool(m, &mcp.Tool{Name: "cancel_transfer", Description: "Revoke a transfer URL and discard staged bytes. Does not undo a device file already published."}, func(_ context.Context, _ *mcp.CallToolRequest, args v2IDArgs) (*mcp.CallToolResult, any, error) {
		if !v.cancel(args.ID) {
			return result(map[string]string{"error": "transfer not found or expired"}, true), nil, nil
		}
		return result(map[string]string{"status": "cancelled"}, false), nil, nil
	})
	mcp.AddTool(m, &mcp.Tool{Name: "list_transfers", Description: "List current binary transfers without download credentials."}, func(_ context.Context, _ *mcp.CallToolRequest, _ noArgs) (*mcp.CallToolResult, any, error) {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.cleanup()
		out := make([]v2Record, 0, len(v.records))
		for _, t := range v.records {
			r := t.v2Record
			r.URL = ""
			out = append(out, r)
		}
		return result(out, false), nil, nil
	})
}
func installV2(m *mcp.Server, h *hub, mux *http.ServeMux, origin, dir string) error {
	if origin == "" {
		return nil
	}
	v, err := newV2Manager(h, origin, dir)
	if err != nil {
		return err
	}
	if v == nil {
		return errors.New("transfer manager unavailable")
	}
	registerV2Tools(m, v)
	mux.HandleFunc(v2Prefix, v.serve)
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for range tick.C {
			v.mu.Lock()
			v.cleanup()
			v.mu.Unlock()
		}
	}()
	return nil
}
