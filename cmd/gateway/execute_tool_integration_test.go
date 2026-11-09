package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExecuteCommandMCPBase64Switch(t *testing.T) {
	h := newHub()
	control := mcp.NewServer(&mcp.Implementation{Name: "test-execute", Version: "1"}, nil)
	registerExecuteTool(control, h)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return control },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	))
	server := httptest.NewServer(mux)
	defer server.Close()

	args := map[string]any{
		"device_id": "test-offline",
		"command":   "invalid!",
		"base64":    true,
	}
	got := testJoinMCP(t, server.URL+"/mcp", "execute_command", args)
	if got["error"] != "invalid Base64 command" {
		t.Fatalf("invalid Base64 accepted: %#v", got)
	}

	args["command"] = base64.StdEncoding.EncodeToString([]byte("Get-Service MySQL80"))
	got = testJoinMCP(t, server.URL+"/mcp", "execute_command", args)
	if got["error"] != "device offline" {
		t.Fatalf("valid Base64 command did not dispatch: %#v", got)
	}

	args["base64"] = false
	args["command"] = "Get-Service MySQL80"
	got = testJoinMCP(t, server.URL+"/mcp", "execute_command", args)
	if got["error"] != "device offline" {
		t.Fatalf("legacy plaintext command no longer dispatches: %#v", got)
	}
}
