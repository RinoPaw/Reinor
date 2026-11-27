package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Effect string

const (
	Read   Effect = "read"
	Action Effect = "action"
)

type Definition struct {
	Name        string             `json:"name"`
	Revision    string             `json:"revision"`
	Description string             `json:"description"`
	Effect      Effect             `json:"effect"`
	Requires    []string           `json:"requires,omitempty"`
	InputSchema *jsonschema.Schema `json:"input_schema,omitempty"`
	Available   *bool              `json:"available,omitempty"`
	Reason      string             `json:"reason,omitempty"`
}
type Spec struct {
	Name     string
	Effect   Effect
	Requires []string
	// Change Version when semantics change without an input schema change.
	Version string
}
type entry struct {
	definition Definition
	call       mcp.ToolHandler
}
type Availability func(spec Definition, device string) (bool, string)
type Registry struct {
	entries      map[string]entry
	native       bool
	availability Availability
}

func New(native bool, availability Availability) *Registry {
	return &Registry{entries: map[string]entry{}, native: native, availability: availability}
}
func Register[T any](r *Registry, server *mcp.Server, spec Spec, tool *mcp.Tool, handler mcp.ToolHandlerFor[T, any]) {
	if spec.Name == "" || !strings.Contains(spec.Name, ".") || (spec.Effect != Read && spec.Effect != Action) || spec.Version == "" {
		panic("invalid operation definition")
	}
	if _, ok := r.entries[spec.Name]; ok {
		panic("duplicate operation: " + spec.Name)
	}
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err)
	}
	schema.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		panic(err)
	}
	d := Definition{Name: spec.Name, Description: tool.Description, Effect: spec.Effect, Requires: spec.Requires, InputSchema: schema}
	b, err := json.Marshal(struct {
		Definition
		Version string
	}{d, spec.Version})
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256(b)
	d.Revision = hex.EncodeToString(hash[:])
	r.entries[d.Name] = entry{definition: d, call: func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		raw := req.Params.Arguments
		var obj map[string]any
		if len(raw) == 0 {
			raw = []byte("{}")
		}
		if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
			return failure("INVALID_ARGUMENTS", "arguments must be an object"), nil
		}
		if err := resolved.Validate(obj); err != nil {
			return failure("INVALID_ARGUMENTS", err.Error()), nil
		}
		var args T
		if err := json.Unmarshal(raw, &args); err != nil {
			return failure("INVALID_ARGUMENTS", err.Error()), nil
		}
		result, output, err := handler(ctx, req, args)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = &mcp.CallToolResult{}
		}
		if output != nil {
			result.StructuredContent = output
		}
		return result, nil
	}}
	if r.native {
		t := *tool
		t.InputSchema = schema
		t.Annotations = annotations(spec.Effect == Read)
		server.AddTool(&t, r.entries[d.Name].call)
	}
}
func annotations(read bool) *mcp.ToolAnnotations {
	destructive := !read
	return &mcp.ToolAnnotations{ReadOnlyHint: read, DestructiveHint: &destructive}
}
func failure(code, message string) *mcp.CallToolResult {
	b, _ := json.Marshal(map[string]string{"code": code, "error": message})
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
func success(value any) *mcp.CallToolResult {
	b, _ := json.Marshal(value)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

type DiscoverArgs struct {
	Namespace string `json:"namespace,omitempty"`
	Operation string `json:"operation,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
}

func (r *Registry) Discover(args DiscoverArgs) []Definition {
	out := make([]Definition, 0, len(r.entries))
	for name, e := range r.entries {
		if args.Namespace != "" && !strings.HasPrefix(name, args.Namespace+".") {
			continue
		}
		if args.Operation != "" && args.Operation != name {
			continue
		}
		d := e.definition

		if args.Namespace == "" && args.Operation == "" {
			d.InputSchema = nil
		}
		if args.DeviceID != "" && len(d.Requires) > 0 && r.availability != nil {
			available, reason := r.availability(d, args.DeviceID)
			d.Available = &available
			d.Reason = reason
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
