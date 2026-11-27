package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RinoPaw/Reinor/internal/operation"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExecuteCommandMCPBase64Switch(t *testing.T) {
	h := newHub()
	control := mcp.NewServer(&mcp.Implementation{Name: "test-execute", Version: "1"}, nil)
	registerExecuteTool(control, h, operation.New(true, nil))
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

func TestDynamicExecuteCommandMCPBase64Switch(t *testing.T) {
	h := newHub()
	control := mcp.NewServer(&mcp.Implementation{Name: "test-dynamic-execute", Version: "1"}, nil)
	reg := operation.New(false, h.operationAvailability)
	registerExecuteTool(control, h, reg)
	reg.Install(control)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server { return control }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true}))
	server := httptest.NewServer(mux)
	defer server.Close()

	endpoint := server.URL + "/mcp"
	catalog := testJoinMCP(t, endpoint, "discover", map[string]any{"operation": "command.execute"})
	ops, ok := catalog["operations"].([]any)
	if !ok || len(ops) != 1 {
		t.Fatalf("unexpected discovery: %#v", catalog)
	}
	def := ops[0].(map[string]any)
	schema := def["input_schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if props["base64"].(map[string]any)["type"] != "boolean" {
		t.Fatalf("missing base64 switch: %#v", schema)
	}
	args := map[string]any{"device_id": "test-offline", "command": "invalid!", "base64": true}
	call := map[string]any{"operation": "command.execute", "revision": def["revision"], "arguments": args}
	if got := testJoinMCP(t, endpoint, "invoke", call); got["error"] != "invalid Base64 command" {
		t.Fatalf("invalid base64 accepted: %#v", got)
	}
	args["command"] = base64.StdEncoding.EncodeToString([]byte("Get-Date"))
	if got := testJoinMCP(t, endpoint, "invoke", call); got["error"] != "device offline" {
		t.Fatalf("valid base64 did not dispatch: %#v", got)
	}
	args["base64"] = false
	args["command"] = "Get-Date"
	if got := testJoinMCP(t, endpoint, "invoke", call); got["error"] != "device offline" {
		t.Fatalf("plaintext did not dispatch: %#v", got)
	}
	delete(args, "base64")
	if got := testJoinMCP(t, endpoint, "invoke", call); got["error"] != "device offline" {
		t.Fatalf("default plaintext did not dispatch: %#v", got)
	}
	if got := testJoinMCP(t, endpoint, "query", call); got["code"] != "WRONG_ENTRYPOINT" {
		t.Fatalf("action accepted by query: %#v", got)
	}
}
