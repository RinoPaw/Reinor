package operation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type sampleArgs struct {
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

func TestDynamicRegistry(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	r := New(false, nil)
	calls := 0
	handler := func(_ context.Context, _ *mcp.CallToolRequest, args sampleArgs) (*mcp.CallToolResult, any, error) {
		calls++
		return success(args), nil, nil
	}
	Register(r, server, Spec{Name: "test.read", Effect: Read, Version: "1"}, &mcp.Tool{Name: "read_sample", Description: "Read sample"}, handler)
	Register(r, server, Spec{Name: "test.write", Effect: Action, Version: "1"}, &mcp.Tool{Name: "write_sample", Description: "Write sample"}, handler)
	r.Install(server)
	index := r.Discover(DiscoverArgs{})
	if len(index) != 2 || index[0].Name != "test.read" || index[0].InputSchema != nil {
		t.Fatalf("bad index: %+v", index)
	}
	defs := r.Discover(DiscoverArgs{Operation: "test.write"})
	if len(defs) != 1 || defs[0].InputSchema == nil || len(defs[0].Revision) != 64 {
		t.Fatalf("bad definition: %+v", defs)
	}
	ctx := context.Background()
	call := func(effect Effect, op, rev, raw string) *mcp.CallToolResult {
		data, _ := json.Marshal(CallArgs{Operation: op, Revision: rev, Arguments: json.RawMessage(raw)})
		got, err := r.call(ctx, &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: data}}, effect)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	rev := defs[0].Revision
	for _, tc := range []struct {
		effect              Effect
		op, rev, args, code string
	}{
		{Read, "test.write", rev, `{"name":"x"}`, "WRONG_ENTRYPOINT"},
		{Action, "test.write", "old", `{"name":"x"}`, "STALE_REVISION"},
		{Action, "test.write", rev, `{}`, "INVALID_ARGUMENTS"},
		{Action, "test.write", rev, `{"name":"x","extra":true}`, "INVALID_ARGUMENTS"},
		{Action, "test.write", rev, `{"name":3}`, "INVALID_ARGUMENTS"},
		{Action, "test.write", rev, `{"name":"x","count":1.5}`, "INVALID_ARGUMENTS"},
		{Action, "test.write", rev, `null`, "INVALID_ARGUMENTS"},
		{Action, "missing.operation", rev, `{}`, "UNKNOWN_OPERATION"},
	} {
		got := call(tc.effect, tc.op, tc.rev, tc.args)
		if !got.IsError || !strings.Contains(got.Content[0].(*mcp.TextContent).Text, tc.code) {
			t.Fatalf("expected %s, got %+v", tc.code, got)
		}
	}
	if calls != 0 {
		t.Fatal("invalid calls invoked handler")
	}
	if got := call(Action, "test.write", rev, `{"name":"x","count":2}`); got.IsError {
		t.Fatal(got)
	}
	if got := call(Read, "test.read", index[0].Revision, `{"name":"x"}`); got.IsError {
		t.Fatal(got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
