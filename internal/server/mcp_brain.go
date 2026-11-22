package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 22v brain tools on FT's MCP connector.
//
// Four read-only tools (brain_search, brain_sheet, brain_read, brain_stats) that forward to the
// private 22v brain connector, a stdlib Python service bound to 127.0.0.1:18765 on this host. FT
// is the only caller; the brain connector is never exposed to the internet. FT's own /mcp
// endpoint (requireReadToken) is what Claude talks to.
//
// The brain is private 22v subscriber material, so in demo mode the tools are hidden from
// tools/list and refuse calls, the same line FT-157 draws for the 22v-* transcripts.
//
// Config (all optional):
//
//	FT_BRAIN_URL         default http://127.0.0.1:18765/mcp; must be a loopback address
//	FT_BRAIN_TOKEN_FILE  default /etc/ft/brain-connector-token (owner ft, mode 0400); read on every
//	                     call, so rotating the token needs no FT restart
const (
	defaultBrainURL       = "http://127.0.0.1:18765/mcp"
	defaultBrainTokenFile = "/etc/ft/brain-connector-token"
	brainMaxResponse      = 256 << 10
	brainToolPrefix       = "brain_"
)

var brainHTTPClient = &http.Client{Timeout: 10 * time.Second}

// brainEndpoint returns the connector URL, refusing anything that is not a loopback http URL: the
// bearer token must never be sent off this machine.
func brainEndpoint() (string, error) {
	raw := os.Getenv("FT_BRAIN_URL")
	if raw == "" {
		raw = defaultBrainURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return "", errors.New("FT_BRAIN_URL must be an http URL on a loopback address")
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
		return "", errors.New("FT_BRAIN_URL must be an http URL on a loopback address")
	}
	return raw, nil
}

func brainToken() (string, error) {
	path := os.Getenv("FT_BRAIN_TOKEN_FILE")
	if path == "" {
		path = defaultBrainTokenFile
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("brain connector token is not readable")
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", errors.New("brain connector token file is empty")
	}
	return t, nil
}

func brainError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}
}

// callBrain forwards one tools/call to the brain connector and returns its text result. Failures
// become a tool error with a fixed message: neither the token nor the connector's body is echoed.
func callBrain(ctx context.Context, tool string, args map[string]any) *mcp.CallToolResult {
	endpoint, err := brainEndpoint()
	if err != nil {
		return brainError(err.Error())
	}
	token, err := brainToken()
	if err != nil {
		return brainError(err.Error())
	}
	if args == nil {
		args = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return brainError("brain connector request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := brainHTTPClient.Do(req)
	if err != nil {
		return brainError("brain connector is not reachable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, brainMaxResponse))
	if err != nil {
		return brainError("brain connector response could not be read")
	}
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return brainError("brain connector rejected the token")
		case http.StatusTooManyRequests:
			return brainError("brain connector rate limit reached, try again in a minute")
		}
		return brainError(fmt.Sprintf("brain connector returned HTTP %d", resp.StatusCode))
	}
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return brainError("brain connector returned an unreadable response")
	}
	if out.Error != nil {
		return brainError("brain connector error: " + out.Error.Message)
	}
	var text strings.Builder
	for _, c := range out.Result.Content {
		text.WriteString(c.Text)
	}
	return &mcp.CallToolResult{IsError: out.Result.IsError, Content: []mcp.Content{&mcp.TextContent{Text: text.String()}}}
}

// --- Tool argument types (mirror the brain connector's input schemas) ---------------------------

type mcpBrainSearchArgs struct {
	Query   string `json:"query" jsonschema:"Word, phrase or several words to find in the private 22v brain (Jordi Visser / 22v transcripts, slide decks, PDFs). Max 300 characters."`
	Section string `json:"section,omitempty" jsonschema:"Optional: restrict to one section of the archive (see brain_stats for the names)."`
	From    string `json:"from,omitempty" jsonschema:"Optional: only documents dated on or after this date (YYYY-MM-DD)."`
	To      string `json:"to,omitempty" jsonschema:"Optional: only documents dated on or before this date (YYYY-MM-DD)."`
	Type    string `json:"type,omitempty" jsonschema:"Optional: restrict to one document type (see brain_stats)."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Optional: maximum hits to return (default 10, maximum 20)."`
}

type mcpBrainSheetArgs struct {
	Term string `json:"term" jsonschema:"Ticker, name or word to find in the 22v spreadsheets (max 30 rows returned)."`
	Date string `json:"date,omitempty" jsonschema:"Optional: only spreadsheets dated this day (YYYY-MM-DD)."`
}

type mcpBrainReadArgs struct {
	DocID     int `json:"doc_id" jsonschema:"Document id from a brain_search hit."`
	ChunkFrom int `json:"chunk_from,omitempty" jsonschema:"Optional: first chunk number to read (default 0)."`
	ChunkTo   int `json:"chunk_to,omitempty" jsonschema:"Optional: last chunk number to read; at most 5 chunks are returned per call."`
}

// brainToolHandler builds the handler for one brain tool: demo mode refuses, otherwise the
// arguments are forwarded as they came in.
func brainToolHandler[A any](s *Server, name string) func(context.Context, *mcp.CallToolRequest, A) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args A) (*mcp.CallToolResult, any, error) {
		if s.demoModeOn(ctx) {
			return brainError("not available in demo mode"), nil, nil
		}
		slog.Info("mcp tool call", "tool", name, "user_id", mcpUserID(ctx))
		var m map[string]any
		if b, err := json.Marshal(args); err == nil {
			_ = json.Unmarshal(b, &m)
		}
		return callBrain(ctx, name, m), nil, nil
	}
}

func (s *Server) registerBrainTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "brain_search",
		Description: "Full-text search over the private 22v brain (Jordi Visser videos, weekly decks, PDFs, charts). Returns ranked hits with date, title, section, source path, a snippet and a page or timestamp; one best passage per document. Private subscriber material, personal use only. Read-only.",
	}, brainToolHandler[mcpBrainSearchArgs](s, "brain_search"))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "brain_sheet",
		Description: "Look up a ticker or word in the 22v spreadsheets: matching rows with the sheet's date and column names (max 30 rows). Private subscriber material. Read-only.",
	}, brainToolHandler[mcpBrainSheetArgs](s, "brain_sheet"))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "brain_read",
		Description: "Read up to 5 neighbouring chunks of one 22v brain document (doc_id from brain_search), to see the text around a hit. Private subscriber material. Read-only.",
	}, brainToolHandler[mcpBrainReadArgs](s, "brain_read"))
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "brain_stats",
		Description: "Size of the 22v brain: document and chunk counts, sections, document types and date ranges. Read-only.",
	}, brainToolHandler[struct{}](s, "brain_stats"))

	// Demo mode hides the tools from tools/list at call time, so a mid-session toggle takes
	// effect at once; the handlers refuse as well.
	srv.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != "tools/list" {
				return res, err
			}
			if lt, ok := res.(*mcp.ListToolsResult); ok && s.demoModeOn(ctx) {
				kept := make([]*mcp.Tool, 0, len(lt.Tools))
				for _, t := range lt.Tools {
					if !strings.HasPrefix(t.Name, brainToolPrefix) {
						kept = append(kept, t)
					}
				}
				lt.Tools = kept
			}
			return res, err
		}
	})
}
