package types

import (
	"encoding/json"
	"testing"
)

// The payloads below are verbatim captures from Claude Code CLI 2.1.220 over
// the control protocol, trimmed only where noted. They exist because each of
// these three decoders was written against the reference SDKs' documented
// shapes and silently produced zero values against the real wire format.

// decodeResponse unmarshals a captured control response body.
func decodeResponse(t *testing.T, payload string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatalf("unmarshal capture: %v", err)
	}
	return m
}

// The CLI keys the selector as "value" and reports the concrete model
// separately as "resolvedModel". It never sends "model".
const listModelsCapture = `{"models":[
  {"value":"default","resolvedModel":"claude-opus-5[1m]","displayName":"Default (recommended)","description":"Opus 5 with 1M context","supportsEffort":true},
  {"value":"opus[1m]","resolvedModel":"claude-opus-5[1m]","displayName":"Opus (1M context)","description":"Opus 5 with 1M context","supportsEffort":true},
  {"value":"claude-fable-5[1m]","resolvedModel":"claude-fable-5[1m]","displayName":"Fable","description":"Fable 5","supportsEffort":true}
]}`

func TestModelInfosFromAnyRealCLI(t *testing.T) {
	models := ModelInfosFromAny(decodeResponse(t, listModelsCapture)["models"])

	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}
	want := []ModelInfo{
		{Model: "default", ResolvedModel: "claude-opus-5[1m]", DisplayName: "Default (recommended)", Description: "Opus 5 with 1M context"},
		{Model: "opus[1m]", ResolvedModel: "claude-opus-5[1m]", DisplayName: "Opus (1M context)", Description: "Opus 5 with 1M context"},
		{Model: "claude-fable-5[1m]", ResolvedModel: "claude-fable-5[1m]", DisplayName: "Fable", Description: "Fable 5"},
	}
	for i, w := range want {
		if models[i] != w {
			t.Errorf("model[%d] = %+v, want %+v", i, models[i], w)
		}
	}
}

func TestModelInfosFromAnyLegacyKeys(t *testing.T) {
	// Older payloads carried the selector as "model", older still as "name".
	models := ModelInfosFromAny([]any{
		map[string]any{"model": "sonnet", "displayName": "Sonnet"},
		map[string]any{"name": "haiku", "displayName": "Haiku"},
	})
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Model != "sonnet" {
		t.Errorf(`legacy "model" key: got %q, want "sonnet"`, models[0].Model)
	}
	if models[1].Model != "haiku" {
		t.Errorf(`legacy "name" key: got %q, want "haiku"`, models[1].Model)
	}
}

// The CLI sends "contents" (plural) and "absPath". It sends no encoding,
// size, or truncation flag, and ignores the maxBytes hint.
const readFileCapture = `{"contents":"module github.com/nabkey/claude-agent-sdk-go\n\ngo 1.24\n","absPath":"/repo/go.mod"}`

func TestReadFileResultFromMapRealCLI(t *testing.T) {
	result := ReadFileResultFromMap(decodeResponse(t, readFileCapture))

	if result == nil {
		t.Fatal("expected a result")
	}
	wantContent := "module github.com/nabkey/claude-agent-sdk-go\n\ngo 1.24\n"
	if result.Content != wantContent {
		t.Errorf("Content = %q, want %q", result.Content, wantContent)
	}
	if result.AbsPath != "/repo/go.mod" {
		t.Errorf("AbsPath = %q, want %q", result.AbsPath, "/repo/go.mod")
	}
}

func TestReadFileResultFromMapSingularKey(t *testing.T) {
	result := ReadFileResultFromMap(map[string]any{"content": "hi"})
	if result.Content != "hi" {
		t.Errorf(`singular "content" key: got %q, want "hi"`, result.Content)
	}
}

// Cost is nested under "session"; rate_limits is an object keyed by window
// name in which most windows are null, carrying non-window siblings
// ("limits", "spend", "extra_usage") alongside the real ones. Timestamps are
// RFC 3339 strings, not epoch numbers.
const getUsageCapture = `{
 "session":{"total_cost_usd":1.25,"total_duration_ms":6178,"model_usage":{}},
 "subscription_type":"max",
 "rate_limits_available":true,
 "rate_limits":{
  "five_hour":{"utilization":8,"resets_at":"2026-07-28T03:39:59.901559+00:00","limit_dollars":null},
  "seven_day":{"utilization":17,"resets_at":"2026-08-01T02:59:59.901579+00:00","limit_dollars":null},
  "seven_day_opus":null,
  "tangelo":null,
  "extra_usage":{"is_enabled":false,"utilization":null,"user_disabled":false},
  "member_dashboard_available":true,
  "limits":[{"kind":"session","group":"session","percent":8,"resets_at":"2026-07-28T03:39:59.901559+00:00"}]
 }
}`

func TestSessionUsageFromMapRealCLI(t *testing.T) {
	usage := SessionUsageFromMap(decodeResponse(t, getUsageCapture))

	if usage.TotalCostUSD != 1.25 {
		t.Errorf("TotalCostUSD = %v, want 1.25 (nested under \"session\")", usage.TotalCostUSD)
	}
	if usage.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q, want %q", usage.SubscriptionType, "max")
	}
	if !usage.RateLimitsAvailable {
		t.Error("RateLimitsAvailable = false, want true")
	}

	// Only the two populated windows: nulls, the non-window siblings, and the
	// "limits" array must all be excluded.
	if len(usage.RateLimits) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(usage.RateLimits), usage.RateLimits)
	}
	// Sorted by name, so five_hour precedes seven_day.
	if usage.RateLimits[0].Type != "five_hour" || usage.RateLimits[1].Type != "seven_day" {
		t.Fatalf("windows not sorted by name: %+v", usage.RateLimits)
	}
	if usage.RateLimits[0].Utilization != 8 {
		t.Errorf("five_hour utilization = %v, want 8", usage.RateLimits[0].Utilization)
	}
	// 2026-07-28T03:39:59+00:00
	const wantResets = int64(1785209999)
	if usage.RateLimits[0].ResetsAt != wantResets {
		t.Errorf("five_hour ResetsAt = %d, want %d (parsed from RFC 3339)",
			usage.RateLimits[0].ResetsAt, wantResets)
	}
	if usage.Raw == nil {
		t.Error("Raw should retain the full response")
	}
}

func TestSessionUsageFromMapLegacyArray(t *testing.T) {
	// An earlier draft shape: an array of windows carrying their own "type"
	// and an epoch-seconds timestamp.
	usage := SessionUsageFromMap(map[string]any{
		"totalCostUSD":        0.5,
		"rateLimitsAvailable": true,
		"rateLimits": []any{
			map[string]any{"type": "five_hour", "utilization": 12.0, "resetsAt": 1785209999.0},
		},
	})
	if usage.TotalCostUSD != 0.5 {
		t.Errorf("TotalCostUSD = %v, want 0.5", usage.TotalCostUSD)
	}
	if len(usage.RateLimits) != 1 {
		t.Fatalf("expected 1 window, got %d", len(usage.RateLimits))
	}
	got := usage.RateLimits[0]
	if got.Type != "five_hour" || got.Utilization != 12 || got.ResetsAt != 1785209999 {
		t.Errorf("window = %+v, want {five_hour 12 1785209999}", got)
	}
}

func TestSessionUsageFromMapNoRateLimits(t *testing.T) {
	// API key and Bedrock sessions report no plan limits at all.
	usage := SessionUsageFromMap(map[string]any{
		"session":               map[string]any{"total_cost_usd": 0.0},
		"rate_limits_available": false,
	})
	if usage.RateLimitsAvailable {
		t.Error("RateLimitsAvailable = true, want false")
	}
	if usage.RateLimits != nil {
		t.Errorf("RateLimits = %+v, want nil", usage.RateLimits)
	}
}
