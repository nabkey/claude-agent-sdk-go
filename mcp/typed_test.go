package mcp

import (
	"context"
	"strings"
	"testing"
)

type addArgs struct {
	A float64 `json:"a" jsonschema:"the first addend"`
	B float64 `json:"b" jsonschema:"the second addend"`
}

// The point of the typed form is that the schema cannot drift from the struct.
func TestNewToolForDerivesSchemaFromStruct(t *testing.T) {
	tool, err := NewToolFor("add", "Add two numbers",
		func(_ context.Context, args addArgs) (map[string]any, error) {
			return TextResult("sum"), nil
		})
	if err != nil {
		t.Fatalf("NewToolFor: %v", err)
	}

	if tool.Name != "add" || tool.Description != "Add two numbers" {
		t.Errorf("unexpected identity: %+v", tool)
	}
	if tool.InputSchema["type"] != "object" {
		t.Errorf("schema type = %v, want object", tool.InputSchema["type"])
	}

	properties, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %v", tool.InputSchema["properties"])
	}
	// Field names come from the json tag, documentation from jsonschema.
	for _, name := range []string{"a", "b"} {
		prop, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatalf("property %q is missing from %v", name, properties)
		}
		if prop["type"] != "number" {
			t.Errorf("property %q type = %v, want number", name, prop["type"])
		}
	}
	if desc, _ := properties["a"].(map[string]any)["description"].(string); !strings.Contains(desc, "first addend") {
		t.Errorf("property a description = %q, want the jsonschema tag", desc)
	}
}

// The handler receives the struct already decoded, honoring the same json tags
// the schema was generated from.
func TestNewToolForDecodesArguments(t *testing.T) {
	var got addArgs
	tool, err := NewToolFor("add", "Add two numbers",
		func(_ context.Context, args addArgs) (map[string]any, error) {
			got = args
			return TextResult("ok"), nil
		})
	if err != nil {
		t.Fatalf("NewToolFor: %v", err)
	}

	if _, err := tool.Handler(context.Background(), map[string]any{
		"a": float64(2), "b": float64(3),
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got.A != 2 || got.B != 3 {
		t.Errorf("decoded args = %+v", got)
	}
}

// Arguments the model got wrong become a tool error the model can read, not a
// transport failure that ends the turn.
func TestNewToolForReportsBadArguments(t *testing.T) {
	tool, err := NewToolFor("add", "Add two numbers",
		func(_ context.Context, args addArgs) (map[string]any, error) {
			t.Error("the handler must not run on undecodable arguments")
			return nil, nil
		})
	if err != nil {
		t.Fatalf("NewToolFor: %v", err)
	}

	result, err := tool.Handler(context.Background(), map[string]any{"a": "not a number"})
	if err != nil {
		t.Fatalf("handler returned a transport error: %v", err)
	}
	if result["isError"] != true {
		t.Errorf("expected an error result, got %v", result)
	}
}

// A struct with no derivable schema is a programming error, so MustToolFor
// exists for package-level definitions where continuing is pointless.
func TestMustToolForPanicsOnUnderivableSchema(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a type with no derivable schema")
		}
	}()

	MustToolFor("bad", "Cannot be described",
		func(_ context.Context, args func()) (map[string]any, error) { return nil, nil })
}

func TestMustToolForReturnsTool(t *testing.T) {
	tool := MustToolFor("add", "Add two numbers",
		func(_ context.Context, args addArgs) (map[string]any, error) {
			return TextResult("ok"), nil
		})
	if tool.Name != "add" || tool.InputSchema == nil {
		t.Errorf("unexpected tool: %+v", tool)
	}
}
