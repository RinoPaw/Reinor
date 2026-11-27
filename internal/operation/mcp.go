package operation

import (
	"context"
	"encoding/json"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log"
)

type CallArgs struct {
	Operation string          `json:"operation"`
	Revision  string          `json:"revision"`
	Arguments json.RawMessage `json:"arguments"`
}

func (r *Registry) call(ctx context.Context, req *mcp.CallToolRequest, effect Effect) (res *mcp.CallToolResult, err error) {
	var args CallArgs
	var obj map[string]json.RawMessage
	if json.Unmarshal(req.Params.Arguments, &obj) != nil || len(obj) != 3 || json.Unmarshal(req.Params.Arguments, &args) != nil || args.Operation == "" || args.Revision == "" || len(args.Arguments) == 0 {
		return failure("INVALID_ARGUMENTS", "operation, revision and arguments are required"), nil
	}
	e, ok := r.entries[args.Operation]
	if !ok {
		return failure("UNKNOWN_OPERATION", "rediscover available operations"), nil
	}
	d := e.definition
	defer func() {
		log.Printf("dynamic operation=%s effect=%s success=%t", d.Name, effect, err == nil && res != nil && !res.IsError)
	}()
	if d.Effect != effect {
		return failure("WRONG_ENTRYPOINT", "use the entrypoint matching the operation effect"), nil
	}
	if args.Revision != d.Revision {
		return failure("STALE_REVISION", "rediscover the operation before calling"), nil
	}
	inner := *req
	params := *req.Params
	params.Name = d.Name
	params.Arguments = args.Arguments
	inner.Params = &params
	return e.call(ctx, &inner)
}
func (r *Registry) Install(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "discover", Description: "Discover Gateway operations. No filters returns an index; filter by namespace or operation for full input schemas and revisions. Device availability is advisory. Use query for read operations and invoke for actions.", Annotations: annotations(true)}, func(_ context.Context, _ *mcp.CallToolRequest, args DiscoverArgs) (*mcp.CallToolResult, any, error) {
		return success(map[string]any{"operations": r.Discover(args)}), nil, nil
	})
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{"operation": {Type: "string"}, "revision": {Type: "string"}, "arguments": {Type: "object"}}, Required: []string{"operation", "revision", "arguments"}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
	for _, effect := range []Effect{Read, Action} {
		name, desc := "query", "Execute a discovered read-only operation with its exact revision and arguments."
		if effect == Action {
			name, desc = "invoke", "Execute a discovered action with its exact revision and arguments. May modify remote devices or disclose data; inspect the operation description and obtain required user confirmation."
		}
		server.AddTool(&mcp.Tool{Name: name, Description: desc, InputSchema: schema, Annotations: annotations(effect == Read)}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return r.call(ctx, req, effect)
		})
	}
}
