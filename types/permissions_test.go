package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Suggestions are meant to be echoed back verbatim as
// PermissionResultAllow.UpdatedPermissions, so a decode that keeps only the
// discriminator silently turns "always allow" into a no-op.
func TestPermissionUpdateRoundTrip(t *testing.T) {
	ruleContent := "ls:*"
	behavior := PermissionBehaviorAllow
	mode := PermissionModeAcceptEdits
	dest := PermissionUpdateDestinationSession

	tests := []struct {
		name string
		in   PermissionUpdate
	}{
		{
			name: "addRules with content",
			in: PermissionUpdate{
				Type:        PermissionUpdateTypeAddRules,
				Rules:       []PermissionRuleValue{{ToolName: "Bash", RuleContent: &ruleContent}},
				Behavior:    &behavior,
				Destination: &dest,
			},
		},
		{
			name: "addRules without rule content",
			in: PermissionUpdate{
				Type:     PermissionUpdateTypeAddRules,
				Rules:    []PermissionRuleValue{{ToolName: "Read"}},
				Behavior: &behavior,
			},
		},
		{
			name: "replaceRules with multiple rules",
			in: PermissionUpdate{
				Type: PermissionUpdateTypeReplaceRules,
				Rules: []PermissionRuleValue{
					{ToolName: "Bash", RuleContent: &ruleContent},
					{ToolName: "Write"},
				},
				Behavior: &behavior,
			},
		},
		{
			name: "setMode",
			in:   PermissionUpdate{Type: PermissionUpdateTypeSetMode, Mode: &mode},
		},
		{
			name: "addDirectories",
			in: PermissionUpdate{
				Type:        PermissionUpdateTypeAddDirectories,
				Directories: []string{"/a", "/b"},
			},
		},
		{
			name: "removeDirectories with destination",
			in: PermissionUpdate{
				Type:        PermissionUpdateTypeRemoveDirectories,
				Directories: []string{"/tmp"},
				Destination: &dest,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// ToMap and FromMap are always separated by JSON on the wire, so
			// the round trip must go through it: ToMap emits typed Go values,
			// whereas FromMap consumes the string/[]any shapes json.Unmarshal
			// produces.
			encoded, err := json.Marshal(tc.in.ToMap())
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			got := PermissionUpdateFromMap(wire)
			if !reflect.DeepEqual(got, tc.in) {
				t.Errorf("round trip lost data:\n got  = %+v\n want = %+v", got, tc.in)
			}
		})
	}
}

// The wire payload is what the CLI actually sends on a can_use_tool request.
func TestPermissionUpdateFromMapWireShape(t *testing.T) {
	wire := map[string]any{
		"type":     "addRules",
		"behavior": "allow",
		"rules": []any{
			map[string]any{"toolName": "Bash", "ruleContent": "git status"},
			map[string]any{"toolName": "Read"},
		},
		"destination": "localSettings",
	}

	got := PermissionUpdateFromMap(wire)

	if got.Type != PermissionUpdateTypeAddRules {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Behavior == nil || *got.Behavior != PermissionBehaviorAllow {
		t.Errorf("Behavior = %v", got.Behavior)
	}
	if got.Destination == nil || *got.Destination != PermissionUpdateDestinationLocalSettings {
		t.Errorf("Destination = %v", got.Destination)
	}
	if len(got.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(got.Rules))
	}
	if got.Rules[0].ToolName != "Bash" || got.Rules[0].RuleContent == nil ||
		*got.Rules[0].RuleContent != "git status" {
		t.Errorf("rule[0] = %+v", got.Rules[0])
	}
	if got.Rules[1].ToolName != "Read" || got.Rules[1].RuleContent != nil {
		t.Errorf("rule[1] = %+v", got.Rules[1])
	}
}

func TestPermissionUpdatesFromAny(t *testing.T) {
	raw := []any{
		map[string]any{"type": "setMode", "mode": "plan"},
		"not an object", // must be skipped, not panic
		map[string]any{"type": "addDirectories", "directories": []any{"/x", 42}},
	}

	got := PermissionUpdatesFromAny(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 decoded updates, got %d", len(got))
	}
	if got[0].Mode == nil || *got[0].Mode != PermissionModePlan {
		t.Errorf("expected plan mode, got %v", got[0].Mode)
	}
	// Non-string directory entries are dropped rather than coerced.
	if !reflect.DeepEqual(got[1].Directories, []string{"/x"}) {
		t.Errorf("Directories = %v", got[1].Directories)
	}
}

func TestPermissionUpdatesFromAnyEmpty(t *testing.T) {
	if got := PermissionUpdatesFromAny(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
