package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// invokeAnthropic makes the actual POST to /v1/messages. Caller has already
// passed the budget gate; this function is concerned only with marshalling
// the request, sending it, and parsing the response.
//
// Returns the message Text, real token counts, and StopReason. Errors at
// transport or non-2xx status are wrapped with context.
func (s *Service) invokeAnthropic(ctx context.Context, model string, req CallRequest, maxOut int) (*anthropicResp, error) {
	body, err := buildAnthropicBody(model, req, maxOut, s.boolPref(ctx, "llm_prompt_caching_enabled", true))
	if err != nil {
		return nil, fmt.Errorf("build body: %w", err)
	}
	url := strings.TrimRight(s.APIBaseURL, "/") + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("x-api-key", s.APIKey)

	resp, err := s.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Truncate the body in error messages so tokens-in-error-text never
		// shows up in logs.
		preview := string(raw)
		if len(preview) > 240 {
			preview = preview[:240] + "…"
		}
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, preview)
	}

	var parsed anthropicMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	out := &anthropicResp{
		StopReason:   parsed.StopReason,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		CacheRead:    parsed.Usage.CacheReadInputTokens,
		CacheWrite:   parsed.Usage.CacheCreationInputTokens,
	}
	// Concatenate text blocks. Anthropic returns content as an array of
	// {type, text} objects; we only care about the text type.
	var b strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	out.Text = b.String()
	return out, nil
}

// anthropicResp is the trimmed shape we hand back to Call.
type anthropicResp struct {
	Text         string
	StopReason   string
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
}

// ----- request marshalling -----------------------------------------------

type anthropicTextBlock struct {
	Type         string                  `json:"type"`
	Text         string                  `json:"text"`
	CacheControl *anthropicCacheControl  `json:"cache_control,omitempty"`
}
type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}
type anthropicMessage struct {
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}
type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// buildAnthropicBody constructs the /v1/messages JSON. Spec 9c.1 D6:
// SystemPrompt + CacheableContext are wrapped with cache_control:ephemeral
// when prompt caching is enabled.
func buildAnthropicBody(model string, req CallRequest, maxOut int, cachingEnabled bool) ([]byte, error) {
	systemBlocks := []anthropicTextBlock{}
	if req.SystemPrompt != "" {
		blk := anthropicTextBlock{Type: "text", Text: req.SystemPrompt}
		if cachingEnabled {
			blk.CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		}
		systemBlocks = append(systemBlocks, blk)
	}
	// CacheableContext gets its own block so it caches independently of
	// system prompt. Two separate cache hits possible.
	if req.CacheableContext != "" {
		blk := anthropicTextBlock{Type: "text", Text: req.CacheableContext}
		if cachingEnabled {
			blk.CacheControl = &anthropicCacheControl{Type: "ephemeral"}
		}
		systemBlocks = append(systemBlocks, blk)
	}

	// Messages: single user turn. v1 disallows tool use so there's no need
	// for a multi-turn conversation here.
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := map[string]any{
		"model":       model,
		"max_tokens":  maxOut,
		"messages":    []msg{{Role: "user", Content: req.UserPrompt}},
	}
	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}
	return json.Marshal(body)
}
