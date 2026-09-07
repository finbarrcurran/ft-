package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SC-41 — Claude ↔ FT MCP connector (read-only, v1).
//
// Six tools, one per data domain, each a thin wrapper around the SAME
// handler the FT dashboard itself calls — invoked in-process via
// httptest.NewRecorder, not a loopback HTTP hop, and with zero duplicated
// query logic. That is the AC-1 guarantee: a tool's output is byte-for-byte
// what the dashboard would see calling that endpoint at the same moment,
// because it IS that endpoint (same demo-mode gating, same store calls).
//
// Read-only is enforced two ways: (1) requireReadToken (middleware.go)
// requires the bearer token's scope to contain "read", and (2) no store
// method capable of a write is ever imported into this file, and the only
// things reachable through the JSON-RPC dispatch are the six tools added
// below — there is no write tool to accidentally expose.
//
// Freshness (D41.5): every response is wrapped in an envelope carrying
// queriedAt (UTC, at call time); per-row freshness fields already present
// in each domain's own payload (locked_date, updated_at, etc.) pass through
// untouched since the payload isn't reshaped.

// callHandlerJSON invokes an existing GET handler in-process and returns its
// decoded JSON body. userID is injected into the context exactly as
// requireUser/requireUserOrToken would.
func (s *Server) callHandlerJSON(ctx context.Context, userID int64, path string, query url.Values, h http.HandlerFunc) (map[string]any, error) {
	target := path
	if enc := query.Encode(); enc != "" {
		target += "?" + enc
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(context.WithValue(ctx, ctxUserID, userID))
	rec := httptest.NewRecorder()
	h(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = map[string]any{}
	}
	out["_httpStatus"] = rec.Code
	return out, nil
}

func mcpResult(data map[string]any) (*mcp.CallToolResult, any, error) {
	env := map[string]any{
		"queriedAt": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	}
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, env, nil
}

func mcpUserID(ctx context.Context) int64 {
	if v, ok := userIDFromContext(ctx); ok {
		return v
	}
	// FT is single-user; requireReadToken already resolved a valid token
	// to a real user before this handler ever runs, so this fallback only
	// guards against context-propagation quirks in the MCP transport, not
	// against an unauthenticated caller.
	return 1
}

// --- Tool argument types ---------------------------------------------------

type mcpThesesArgs struct {
	Adapter string `json:"adapter,omitempty" jsonschema:"Optional adapter slug to filter stock theses (e.g. ai_infra_semi). Omit for all stock theses; crypto theses are always returned in full."`
}

type mcpAlertsArgs struct {
	OnlyUnnotified bool `json:"only_unnotified,omitempty" jsonschema:"If true (default), only alerts not yet notified today. Set false for the full current alert set."`
}

type mcpPerformanceArgs struct {
	View   string `json:"view,omitempty" jsonschema:"One of overview, cohorts, calibration. Defaults to overview."`
	Window string `json:"window,omitempty" jsonschema:"Time window for the overview view (e.g. all, 1y, 90d). Defaults to all. Ignored for cohorts/calibration."`
}

// --- Tool registration -------------------------------------------------

// mountMCP registers the SC-41 read-only MCP server and mounts it at /mcp,
// gated by requireReadToken. Called once from routes().
func (s *Server) mountMCP() {
	impl := &mcp.Implementation{Name: "ft", Version: "1.0.0"}
	srv := mcp.NewServer(impl, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_theses",
		Description: "Live locked-thesis data — stock (theses_index) and crypto (crypto_theses), current locked version only. Read-only, no cache; matches the FT Theses tab exactly at call time.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpThesesArgs) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		slog.Info("mcp tool call", "tool", "get_theses", "user_id", userID)
		q := url.Values{}
		if args.Adapter != "" {
			q.Set("adapter", args.Adapter)
		}
		stock, err := s.callHandlerJSON(ctx, userID, "/api/theses", q, s.handleListTheses)
		if err != nil {
			return nil, nil, err
		}
		crypto, err := s.callHandlerJSON(ctx, userID, "/api/crypto/theses", nil, s.handleCryptoThesesList)
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(map[string]any{"stock": stock, "crypto": crypto})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_scorecards",
		Description: "Live sector-adapter scorecards — stock (sector_scorecards) and crypto (crypto_adapters), full current-version rubric text per adapter. Read-only, no cache.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		slog.Info("mcp tool call", "tool", "get_scorecards", "user_id", userID)
		stock, err := s.callHandlerJSON(ctx, userID, "/api/scorecards", nil, s.handleScorecardsList)
		if err != nil {
			return nil, nil, err
		}
		crypto, err := s.callHandlerJSON(ctx, userID, "/api/crypto/adapters", nil, s.handleCryptoAdaptersList)
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(map[string]any{"stock": stock, "crypto": crypto})
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_gap_report",
		Description: "Live thesis coverage gaps — held/watchlisted tickers without a current locked thesis. Read-only, no cache.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		slog.Info("mcp tool call", "tool", "get_gap_report", "user_id", userID)
		data, err := s.callHandlerJSON(ctx, userID, "/api/theses/gaps", nil, s.handleThesesGaps)
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(data)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_alerts",
		Description: "Live holding alerts (RED/AMBER/GREEN triggers on stock holdings) — the same feed the Telegram bot reads. Read-only, no cache.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpAlertsArgs) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		slog.Info("mcp tool call", "tool", "get_alerts", "user_id", userID)
		q := url.Values{}
		if !args.OnlyUnnotified {
			q.Set("only_unnotified", "0")
		}
		data, err := s.callHandlerJSON(ctx, userID, "/api/bot/alerts", q, s.handleBotAlerts)
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(data)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_performance",
		Description: "Live performance data — overview (equity curve + metrics), cohorts, or calibration view. Read-only, no cache; demo-masked exactly like the dashboard when demo mode is on.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args mcpPerformanceArgs) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		view := args.View
		if view == "" {
			view = "overview"
		}
		slog.Info("mcp tool call", "tool", "get_performance", "user_id", userID, "view", view)
		var (
			data map[string]any
			err  error
		)
		switch view {
		case "cohorts":
			data, err = s.callHandlerJSON(ctx, userID, "/api/performance/cohorts", nil, s.handlePerformanceCohorts)
		case "calibration":
			data, err = s.callHandlerJSON(ctx, userID, "/api/performance/calibration", nil, s.handlePerformanceCalibration)
		default:
			q := url.Values{}
			if args.Window != "" {
				q.Set("window", args.Window)
			}
			data, err = s.callHandlerJSON(ctx, userID, "/api/performance/overview", q, s.handlePerformanceOverview)
		}
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(data)
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_watchlist",
		Description: "Live FT watchlist (target-entry tracking; distinct from the eToro watchlist) with latest score per entry. Read-only, no cache.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		userID := mcpUserID(ctx)
		slog.Info("mcp tool call", "tool", "get_watchlist", "user_id", userID)
		data, err := s.callHandlerJSON(ctx, userID, "/api/watchlist", nil, s.handleListWatchlist)
		if err != nil {
			return nil, nil, err
		}
		return mcpResult(data)
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		// FT binds 127.0.0.1 only and is reached exclusively via the
		// Cloudflare Tunnel, which connects from a localhost address but
		// preserves the public Host header (ft.curranhouse.dev). The SDK's
		// default DNS-rebinding guard treats that exact combination as
		// suspicious and 403s it — but the threat it guards against (a
		// browser script hitting a localhost MCP server) doesn't apply to a
		// server-to-server bearer-token client, and requireReadToken already
		// gates this path. Left enabled, every real request through the
		// tunnel would be rejected.
		DisableLocalhostProtection: true,
	})
	s.mux.HandleFunc("/mcp", s.requireReadToken(handler.ServeHTTP)) // SC-41
}
