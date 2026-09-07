package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestResultHelpers(t *testing.T) {
	tests := []struct {
		name    string
		result  map[string]any
		want    map[string]any
		isError bool
	}{
		{
			name:   "text",
			result: TextResult("hello"),
			want:   map[string]any{"type": "text", "text": "hello"},
		},
		{
			name:    "error",
			result:  ErrorResult("division by zero"),
			want:    map[string]any{"type": "text", "text": "division by zero"},
			isError: true,
		},
		{
			name:   "image",
			result: ImageResult("YmFzZTY0", "image/png"),
			want:   map[string]any{"type": "image", "data": "YmFzZTY0", "mimeType": "image/png"},
		},
		{
			name:   "audio",
			result: AudioResult("YmFzZTY0", "audio/wav"),
			want:   map[string]any{"type": "audio", "data": "YmFzZTY0", "mimeType": "audio/wav"},
		},
		{
			name:   "resource link",
			result: ResourceLinkResult("out.csv", "file:///tmp/out.csv", "the results"),
			want: map[string]any{
				"type": "resource_link", "uri": "file:///tmp/out.csv",
				"name": "out.csv", "description": "the results",
			},
		},
		{
			name:   "embedded resource",
			result: EmbeddedResourceResult("file:///a.txt", "text/plain", "body"),
			want: map[string]any{
				"type":     "resource",
				"resource": map[string]any{"uri": "file:///a.txt", "mimeType": "text/plain", "text": "body"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, ok := tc.result["content"].([]map[string]any)
			if !ok || len(content) != 1 {
				t.Fatalf("content = %v", tc.result["content"])
			}
			if !reflect.DeepEqual(content[0], tc.want) {
				t.Errorf("content[0] =\n got  %v\n want %v", content[0], tc.want)
			}
			if flag, _ := tc.result["isError"].(bool); flag != tc.isError {
				t.Errorf("isError = %v, want %v", flag, tc.isError)
			}
		})
	}
}

// Optional fields are omitted rather than sent empty, so a client rendering a
// resource link does not show a blank name.
func TestResourceLinkResultOmitsEmptyFields(t *testing.T) {
	content, _ := ResourceLinkResult("", "file:///a", "")["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", content)
	}
	link := content[0]

	for _, key := range []string{"name", "description"} {
		if _, ok := link[key]; ok {
			t.Errorf("empty %q must be omitted, got %v", key, link[key])
		}
	}
}

func TestEmbeddedResourceResultOmitsEmptyMIMEType(t *testing.T) {
	content, _ := EmbeddedResourceResult("file:///a", "", "body")["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", content)
	}
	resource, _ := content[0]["resource"].(map[string]any)

	if _, ok := resource["mimeType"]; ok {
		t.Error("an empty mimeType must be omitted")
	}
}

func TestMultiResult(t *testing.T) {
	combined := MultiResult(TextResult("one"), TextResult("two"))

	content, _ := combined["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("expected two blocks, got %v", content)
	}
	if content[0]["text"] != "one" || content[1]["text"] != "two" {
		t.Errorf("blocks are out of order: %v", content)
	}
	if _, ok := combined["isError"]; ok {
		t.Error("isError must be omitted when nothing failed")
	}
}

// One failing part makes the whole result an error, or a caller could act on
// a partial success.
func TestMultiResultPropagatesError(t *testing.T) {
	combined := MultiResult(TextResult("ok"), ErrorResult("boom"))

	if combined["isError"] != true {
		t.Error("expected isError to propagate")
	}
	content, _ := combined["content"].([]map[string]any)
	if len(content) != 2 {
		t.Error("expected both blocks to survive")
	}
}

// Results assembled from decoded payloads carry []any content, which has to
// combine the same way as the helpers' []map[string]any.
func TestMultiResultAcceptsRoundTrippedContent(t *testing.T) {
	encoded, err := json.Marshal(TextResult("decoded"))
	if err != nil {
		t.Fatal(err)
	}
	var roundTripped map[string]any
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatal(err)
	}

	combined := MultiResult(TextResult("direct"), roundTripped)
	content, _ := combined["content"].([]map[string]any)
	if len(content) != 2 {
		t.Fatalf("expected two blocks, got %v", content)
	}
	if content[1]["text"] != "decoded" {
		t.Errorf("round-tripped block = %v", content[1])
	}
}

func TestMultiResultSkipsUnusableContent(t *testing.T) {
	combined := MultiResult(
		TextResult("kept"),
		map[string]any{"content": "not a list"},
		map[string]any{},
		map[string]any{"content": []any{"not an object"}},
	)

	content, _ := combined["content"].([]map[string]any)
	if len(content) != 1 {
		t.Errorf("expected only the usable block, got %d", len(content))
	}
}

// A tool handler receives whatever the model produced, so the accessors have
// to report a missing or wrongly-typed argument rather than panicking.
func TestArgumentAccessors(t *testing.T) {
	args := map[string]any{
		"text": "hello",
		"num":  float64(3.5),
		"int":  float64(7),
		"flag": true,
	}

	if got, err := GetString(args, "text"); err != nil || got != "hello" {
		t.Errorf("GetString = (%q, %v)", got, err)
	}
	if got, err := GetFloat(args, "num"); err != nil || got != 3.5 {
		t.Errorf("GetFloat = (%v, %v)", got, err)
	}
	if got, err := GetInt(args, "int"); err != nil || got != 7 {
		t.Errorf("GetInt = (%v, %v)", got, err)
	}
	if got, err := GetBool(args, "flag"); err != nil || !got {
		t.Errorf("GetBool = (%v, %v)", got, err)
	}

	if _, err := GetString(args, "missing"); err == nil {
		t.Error("expected a missing-parameter error")
	}
	if _, err := GetString(args, "num"); err == nil {
		t.Error("expected a type error for a number read as a string")
	}
	if _, err := GetFloat(args, "text"); err == nil {
		t.Error("expected a type error for a string read as a number")
	}
	if _, err := GetBool(args, "text"); err == nil {
		t.Error("expected a type error for a string read as a bool")
	}
}

// JSON numbers decode to float64, but a hand-built map may hold any numeric
// type, so both are accepted.
func TestGetFloatAcceptsEveryNumericType(t *testing.T) {
	for name, value := range map[string]any{
		"float64": float64(2), "float32": float32(2), "int": int(2), "int64": int64(2),
	} {
		got, err := GetFloat(map[string]any{"n": value}, "n")
		if err != nil || got != 2 {
			t.Errorf("%s: GetFloat = (%v, %v)", name, got, err)
		}
	}
}

func TestOptionalAccessorsFallBack(t *testing.T) {
	args := map[string]any{"text": "set", "num": float64(1)}

	if got := GetStringOptional(args, "text", "fallback"); got != "set" {
		t.Errorf("GetStringOptional = %q", got)
	}
	if got := GetStringOptional(args, "missing", "fallback"); got != "fallback" {
		t.Errorf("GetStringOptional = %q, want the default", got)
	}
	// A wrongly-typed value falls back rather than erroring: these accessors
	// exist so a handler can keep going.
	if got := GetStringOptional(args, "num", "fallback"); got != "fallback" {
		t.Errorf("GetStringOptional = %q, want the default", got)
	}
	if got := GetFloatOptional(args, "num", 9); got != 1 {
		t.Errorf("GetFloatOptional = %v", got)
	}
	if got := GetFloatOptional(args, "text", 9); got != 9 {
		t.Errorf("GetFloatOptional = %v, want the default", got)
	}
}

func TestNewToolSimpleDerivesSchema(t *testing.T) {
	tool := NewToolSimple("add", "Add numbers", map[string]any{
		"a": float64(0), "b": 0, "label": "", "flag": false,
	}, nil)

	schema := tool.InputSchema
	if schema["type"] != "object" {
		t.Errorf("type = %v, want object", schema["type"])
	}

	properties, _ := schema["properties"].(map[string]any)
	want := map[string]string{"a": "number", "b": "integer", "label": "string", "flag": "boolean"}
	for name, wantType := range want {
		prop, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatalf("property %q is missing", name)
		}
		if prop["type"] != wantType {
			t.Errorf("property %q type = %v, want %v", name, prop["type"], wantType)
		}
	}

	// Every parameter in the simple form is required.
	required, _ := schema["required"].([]string)
	if got := len(required); got != len(want) {
		t.Errorf("required has %d entries, want %d", got, len(want))
	}
}

// The MCP schema strips unknown annotation fields, so the size cap has to ride
// in the Anthropic-namespaced _meta instead.
func TestToolMetaCarriesMaxResultSize(t *testing.T) {
	meta := Tool{Name: "t", MaxResultSizeChars: 4096}.meta(false)
	if meta["anthropic/maxResultSizeChars"] != 4096 {
		t.Errorf("meta = %v", meta)
	}

	if (Tool{Name: "t"}).meta(false) != nil {
		t.Error("an unset cap must produce no _meta at all")
	}
	if (Tool{Name: "t", MaxResultSizeChars: -1}).meta(false) != nil {
		t.Error("a nonsensical cap must produce no _meta")
	}
}

// A tool is always loaded when either it or its server says so.
func TestToolMetaAlwaysLoad(t *testing.T) {
	tests := []struct {
		name             string
		tool             Tool
		serverAlwaysLoad bool
		want             bool
	}{
		{"neither", Tool{Name: "t"}, false, false},
		{"per tool", Tool{Name: "t", AlwaysLoad: true}, false, true},
		{"per server", Tool{Name: "t"}, true, true},
		{"both", Tool{Name: "t", AlwaysLoad: true}, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			meta := tc.tool.meta(tc.serverAlwaysLoad)
			got, _ := meta["anthropic/alwaysLoad"].(bool)
			if got != tc.want {
				t.Errorf("alwaysLoad = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNewToolWithAnnotations(t *testing.T) {
	readOnly := true
	tool := NewToolWithAnnotations("read", "Read a file",
		map[string]any{"type": "object"}, nil, &ToolAnnotations{
			Title:        "Read",
			ReadOnlyHint: &readOnly,
		})

	if tool.Annotations == nil || tool.Annotations.Title != "Read" {
		t.Fatalf("annotations = %+v", tool.Annotations)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("expected the read-only hint to be carried")
	}
}

// Unset hints must stay off the wire: a client cannot tell "not destructive"
// from "no opinion" if every hint is serialized as false.
func TestToolAnnotationsOmitUnsetHints(t *testing.T) {
	encoded, err := json.Marshal(&ToolAnnotations{Title: "Read"})
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
		if _, ok := decoded[key]; ok {
			t.Errorf("unset hint %q must be omitted", key)
		}
	}
}

func TestNewToolHandlerRuns(t *testing.T) {
	tool := NewTool("greet", "Greet someone", map[string]any{"type": "object"},
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			name, err := GetString(args, "name")
			if err != nil {
				return ErrorResult(err.Error()), nil
			}
			return TextResult("Hello, " + name), nil
		})

	result, err := tool.Handler(context.Background(), map[string]any{"name": "Ada"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	content, _ := result["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", result["content"])
	}
	if got := content[0]["text"]; got != "Hello, Ada" {
		t.Errorf("text = %v", got)
	}

	// A missing argument becomes a tool error the model can read, not a
	// transport failure.
	result, err = tool.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if result["isError"] != true {
		t.Errorf("expected an error result, got %v", result)
	}
}
