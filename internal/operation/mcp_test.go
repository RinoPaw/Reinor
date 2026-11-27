package operation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStableMCPContract(t *testing.T) {
	ctx := context.Background()
	var baseline []byte
	for _, version := range []string{"1", "2"} {
		server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
		r := New(false, nil)
		handler := func(_ context.Context, _ *mcp.CallToolRequest, args sampleArgs) (*mcp.CallToolResult, any, error) {
			return success(args), nil, nil
		}
		Register(r, server, Spec{Name: "sample.read", Effect: Read, Version: "1"}, &mcp.Tool{Name: "sample_read", Description: "Read sample"}, handler)
		if version == "2" {
			Register(r, server, Spec{Name: "sample.new", Effect: Action, Version: "1"}, &mcp.Tool{Name: "sample_new", Description: "New action"}, handler)
		}
		r.Install(server)
		st, ct := mcp.NewInMemoryTransports()
		session, err := server.Connect(ctx, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
		cs, err := client.Connect(ctx, ct, nil)
		if err != nil {
			session.Close()
			t.Fatal(err)
		}
		listed, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(listed.Tools) != 3 {
			t.Fatalf("expected 3 fixed tools, got %d", len(listed.Tools))
		}
		current, err := json.Marshal(listed.Tools)
		if err != nil {
			t.Fatal(err)
		}
		if baseline == nil {
			baseline = current
		} else if string(current) != string(baseline) {
			t.Fatal("fixed MCP contract changed after adding an operation")
		}
		if version == "2" {
			out, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "discover", Arguments: map[string]any{"operation": "sample.new"}})
			if err != nil || out.IsError {
				t.Fatalf("discover: %+v %v", out, err)
			}
			var catalog struct {
				Operations []Definition `json:"operations"`
			}
			if err = json.Unmarshal([]byte(out.Content[0].(*mcp.TextContent).Text), &catalog); err != nil || len(catalog.Operations) != 1 {
				t.Fatalf("catalog: %+v %v", catalog, err)
			}
			out, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "invoke", Arguments: map[string]any{"operation": "sample.new", "revision": catalog.Operations[0].Revision, "arguments": map[string]any{"name": "discovered"}}})
			if err != nil || out.IsError {
				t.Fatalf("invoke: %+v %v", out, err)
			}
			var value sampleArgs
			if err = json.Unmarshal([]byte(out.Content[0].(*mcp.TextContent).Text), &value); err != nil || value.Name != "discovered" {
				t.Fatalf("unexpected output: %+v %v", value, err)
			}
		}
		cs.Close()
		session.Close()
	}
}
