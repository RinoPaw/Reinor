package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RinoPaw/Reinor/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxFileChunk = 128 << 10

type fileStartArgs struct {
	DeviceID string `json:"device_id" jsonschema:"Enrolled device ID from list_devices"`
	Path     string `json:"path" jsonschema:"Absolute destination path on the device; existing files cannot be overwritten"`
	Size     uint64 `json:"size" jsonschema:"Exact file size in bytes (maximum 4 GiB)"`
	SHA256   string `json:"sha256" jsonschema:"SHA-256 hex digest of the complete file"`
}
type fileChunkArgs struct {
	DeviceID   string `json:"device_id"`
	TransferID string `json:"transfer_id"`
	Offset     uint64 `json:"offset" jsonschema:"Byte offset; must match next_offset from the previous response"`
	DataBase64 string `json:"data_base64" jsonschema:"Base64-encoded raw binary chunk, max 128 KiB decoded"`
}
type fileTransferArgs struct {
	DeviceID   string `json:"device_id"`
	TransferID string `json:"transfer_id"`
}
type filePathArgs struct {
	DeviceID string `json:"device_id"`
	Path     string `json:"path" jsonschema:"Absolute path on the enrolled device"`
}
type fileReadArgs struct {
	DeviceID string `json:"device_id"`
	Path     string `json:"path" jsonschema:"Absolute path on the enrolled device"`
	Offset   uint64 `json:"offset" jsonschema:"Byte offset to read; each response returns at most 128 KiB"`
}

func newRequestID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// fileRequest reuses the existing agent task/response channel. Only an Agent
// that explicitly advertises file-v1 receives file operations, so older
// deployed agents fail fast instead of silently timing out.
func (h *hub) fileRequest(ctx context.Context, deviceID string, frame wire.Envelope) (wire.Envelope, error) {
	if !validDeviceID.MatchString(deviceID) {
		return wire.Envelope{}, errors.New("invalid device_id")
	}
	h.mu.Lock()
	a := h.agents[deviceID]
	h.mu.Unlock()
	if a == nil {
		return wire.Envelope{}, errors.New("device offline")
	}
	if !a.fileTransfer {
		return wire.Envelope{}, errors.New("agent has no file-v1 capability; update the agent first")
	}
	if !a.reserve() {
		return wire.Envelope{}, errors.New("device busy")
	}
	defer a.release()

	id, err := newRequestID()
	if err != nil {
		return wire.Envelope{}, err
	}
	frame.TaskID = id
	response := make(chan wire.Envelope, 1)
	h.mu.Lock()
	h.pending[id] = pending{agent: a, ch: response}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }()

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = a.send(sendCtx, frame)
	cancel()
	if err != nil {
		return wire.Envelope{}, fmt.Errorf("dispatch failed; operation status unknown: %w", err)
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case r := <-response:
		if r.Error != "" {
			return r, errors.New(r.Error)
		}
		if r.Type != "file_result" {
			return wire.Envelope{}, errors.New("invalid agent response type")
		}
		return r, nil
	case <-timer.C:
		return wire.Envelope{}, errors.New("file operation timed out; status unknown")
	case <-ctx.Done():
		return wire.Envelope{}, fmt.Errorf("request canceled; file operation status unknown: %w", ctx.Err())
	}
}

func registerFileTools(m *mcp.Server, h *hub) {
	mcp.AddTool(m, &mcp.Tool{Name: "begin_upload", Description: "Begin an owner-authorized binary upload to a device. No overwrite. Returns a transfer ID; use upload_chunk then finish_upload."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args fileStartArgs) (*mcp.CallToolResult, any, error) {
			id, err := newRequestID()
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			r, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{
				Type: "file_start", TransferID: id, Path: args.Path, Size: args.Size, SHA256: strings.ToLower(args.SHA256),
			})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]any{"transfer_id": r.TransferID, "path": r.Path, "size": r.Size, "chunk_bytes": maxFileChunk}, false), nil, nil
		})
	mcp.AddTool(m, &mcp.Tool{Name: "upload_chunk", Description: "Append up to 128 KiB of base64-decoded binary data to an upload. Repeated matching chunks are safe to retry."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args fileChunkArgs) (*mcp.CallToolResult, any, error) {
			if len(args.DataBase64) > base64.StdEncoding.EncodedLen(maxFileChunk) {
				return result(map[string]string{"error": "encoded chunk exceeds limit"}, true), nil, nil
			}
			data, err := base64.StdEncoding.DecodeString(args.DataBase64)
			if err != nil {
				return result(map[string]string{"error": "invalid base64"}, true), nil, nil
			}
			r, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{Type: "file_chunk", TransferID: args.TransferID, Offset: args.Offset, Data: data})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]any{"transfer_id": r.TransferID, "next_offset": r.Offset, "size": r.Size}, false), nil, nil
		})
	mcp.AddTool(m, &mcp.Tool{Name: "finish_upload", Description: "Verify complete SHA-256 and atomically publish an uploaded file without overwriting an existing path."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args fileTransferArgs) (*mcp.CallToolResult, any, error) {
			r, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{Type: "file_finish", TransferID: args.TransferID})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]any{"path": r.Path, "size": r.Size, "sha256": r.SHA256}, false), nil, nil
		})
	mcp.AddTool(m, &mcp.Tool{Name: "abort_upload", Description: "Discard an incomplete staged upload and its temporary file."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args fileTransferArgs) (*mcp.CallToolResult, any, error) {
			_, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{Type: "file_abort", TransferID: args.TransferID})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]bool{"aborted": true}, false), nil, nil
		})
	mcp.AddTool(m, &mcp.Tool{Name: "get_file_info", Description: "Inspect a regular file on an enrolled device and compute its full SHA-256 before download."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args filePathArgs) (*mcp.CallToolResult, any, error) {
			r, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{Type: "file_info", Path: args.Path})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]any{"size": r.Size, "sha256": r.SHA256}, false), nil, nil
		})
	mcp.AddTool(m, &mcp.Tool{Name: "download_chunk", Description: "Read at most 128 KiB from a device file. Returns base64 bytes and each chunk's SHA-256; compare assembled file against get_file_info."},
		func(ctx context.Context, _ *mcp.CallToolRequest, args fileReadArgs) (*mcp.CallToolResult, any, error) {
			r, err := h.fileRequest(ctx, args.DeviceID, wire.Envelope{Type: "file_read", Path: args.Path, Offset: args.Offset})
			if err != nil {
				return result(map[string]string{"error": err.Error()}, true), nil, nil
			}
			return result(map[string]any{"offset": r.Offset, "size": r.Size, "data_base64": base64.StdEncoding.EncodeToString(r.Data), "sha256_chunk": r.SHA256}, false), nil, nil
		})
}
