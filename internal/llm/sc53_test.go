package llm

import (
	"encoding/json"
	"testing"
)

func TestFeatureOutputCap(t *testing.T) {
	if got := effectiveOutputCap("video_items", 4000); got != 8000 {
		t.Fatalf("video_items cap = %d, want 8000", got)
	}
	if got := effectiveOutputCap("video_items", 12000); got != 12000 {
		t.Fatalf("a higher global cap must win, got %d", got)
	}
	if got := effectiveOutputCap("video_digest", 4000); got != 4000 {
		t.Fatalf("other features keep the global cap, got %d", got)
	}
}

func TestThinkingOffBody(t *testing.T) {
	b, err := buildAnthropicBody("claude-haiku-5-5", CallRequest{FeatureID: "video_items", UserPrompt: "x", ThinkingOff: true}, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	th, _ := m["thinking"].(map[string]any)
	if th["type"] != "disabled" {
		t.Fatalf("thinking = %v", m["thinking"])
	}
	b, _ = buildAnthropicBody("claude-haiku-4-5", CallRequest{FeatureID: "video_digest", UserPrompt: "x"}, 100, false)
	m = map[string]any{}
	_ = json.Unmarshal(b, &m)
	if _, ok := m["thinking"]; ok {
		t.Fatal("thinking must be absent unless asked for")
	}
}
