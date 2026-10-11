package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-trade-bot/internal/configuration"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func forwarderFor(srv *httptest.Server, token string) *apiForwarder {
	return newAPIForwarder(&configuration.Configuration{MCPAPIURL: srv.URL + "/", APIToken: token})
}

func toolNamed(t *testing.T, name string) apiTool {
	t.Helper()
	for _, tool := range apiTools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return apiTool{}
}

// The table is the security boundary (the bearer is a synthetic admin), so
// the exact set of non-GET routes is pinned: adding a write means editing
// this test on purpose.
func TestAPITools_WriteSetIsPinned(t *testing.T) {
	want := map[string]bool{
		"POST /backtest/{id}/montecarlo":          true,
		"POST /script/repl":                       true,
		"POST /script/fast-rerun":                 true,
		"POST /candle-datasets":                   true,
		"POST /candle-datasets/{id}/pause":        true,
		"POST /candle-datasets/{id}/resume":       true,
		"POST /candle-datasets/{id}/retry-failed": true,
		"POST /candle-datasets/{id}/reconcile":    true,
		"POST /agents/{id}/pause":                 true,
		"POST /agents/{id}/run":                   true,
	}
	got := map[string]bool{}
	for _, tool := range apiTools {
		if tool.Method != http.MethodGet {
			got[tool.Method+" "+tool.Path] = true
		}
	}
	assert.Equal(t, want, got)
}

// Tier 3 and identity routes must never appear, whatever the method.
func TestAPITools_NeverExposesTier3(t *testing.T) {
	for _, tool := range apiTools {
		assert.NotContains(t, tool.Path, "/users", tool.Name)
		assert.NotContains(t, tool.Path, "/auth", tool.Name)
		assert.NotContains(t, tool.Path, "/approve", tool.Name)
		assert.NotContains(t, tool.Path, "/reject", tool.Name)
		assert.NotContains(t, tool.Path, "/deploy-gate", tool.Name)
		assert.NotContains(t, tool.Path, "/signal/close", tool.Name)
		assert.NotContains(t, tool.Path, "/status", tool.Name)
		assert.NotContains(t, tool.Path, "/mode", tool.Name)
		if tool.Method != http.MethodGet {
			assert.False(t, strings.HasPrefix(tool.Path, "/settings"), tool.Name)
			assert.False(t, strings.HasPrefix(tool.Path, "/account"), tool.Name)
			assert.False(t, strings.HasPrefix(tool.Path, "/strategy"), tool.Name)
			assert.False(t, strings.HasPrefix(tool.Path, "/webhook-targets"), tool.Name)
			assert.NotEqual(t, "/agents/kill-switch", tool.Path, tool.Name)
		}
	}
}

func TestAPITools_NamesUniqueAndSchemasValid(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range apiTools {
		assert.False(t, seen[tool.Name], "duplicate tool name %s", tool.Name)
		seen[tool.Name] = true
		var schema map[string]any
		require.NoError(t, json.Unmarshal(tool.inputSchema(), &schema), tool.Name)
		assert.Equal(t, "object", schema["type"], tool.Name)
		for _, p := range tool.Params {
			assert.Contains(t, tool.Path, "{"+p+"}", tool.Name)
		}
	}
}

func TestForwarder_GETWithPathQueryAndAuth(t *testing.T) {
	var gotPath, gotQuery, gotAuth, gotXRW, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotAuth, gotXRW = r.Header.Get("Authorization"), r.Header.Get("X-Requested-With")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	out, err := forwarderFor(srv, "s3cret").call(context.Background(), toolNamed(t, "list_candle_chunks"),
		json.RawMessage(`{"id": 7, "query": {"status": "dead", "limit": 20}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, out)
	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/api/candle-datasets/7/chunks", gotPath)
	assert.Equal(t, "limit=20&status=dead", gotQuery)
	assert.Equal(t, "Bearer s3cret", gotAuth)
	assert.Equal(t, "gtb", gotXRW)
}

func TestForwarder_POSTSendsBody_NoAuthHeaderWithoutToken(t *testing.T) {
	var gotBody, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth, gotCT = string(b), r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := forwarderFor(srv, "").call(context.Background(), toolNamed(t, "pause_agent"),
		json.RawMessage(`{"id": "3", "body": {"paused": true}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"paused":true}`, gotBody)
	assert.Equal(t, "", gotAuth)
	assert.Equal(t, "application/json", gotCT)

	_, err = forwarderFor(srv, "").call(context.Background(), toolNamed(t, "run_agent"), json.RawMessage(`{"id": 3}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, gotBody, "missing body defaults to an empty object")
}

func TestForwarder_RejectsBadIDsBeforeAnyRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	f := forwarderFor(srv, "t")

	for _, args := range []string{`{}`, `{"id": "../../settings"}`, `{"id": "1/../2"}`, `{"id": -1}`, `{"id": 1.5}`, `{"id": "x"}`} {
		_, err := f.call(context.Background(), toolNamed(t, "get_signal"), json.RawMessage(args))
		assert.Error(t, err, args)
	}
	assert.False(t, called, "an invalid id must never reach the API")
}

func TestForwarder_NonSuccessBecomesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := forwarderFor(srv, "t").call(context.Background(), toolNamed(t, "get_signal"), json.RawMessage(`{"id": 9}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 404")
	assert.Contains(t, err.Error(), "not found")
}

func TestForwarder_TruncatesHugeResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxAPIResponseBytes+500)))
	}))
	defer srv.Close()
	out, err := forwarderFor(srv, "t").call(context.Background(), toolNamed(t, "list_signals"), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Contains(t, out, "[truncated")
	assert.Less(t, len(out), maxAPIResponseBytes+200)
}

func TestForwarder_APIUnreachableIsActionable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	f := newAPIForwarder(&configuration.Configuration{MCPAPIURL: url})
	_, err := f.call(context.Background(), toolNamed(t, "get_settings"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP.API_URL")
}

// End to end over the SDK's in-memory transport: the tools are listed, a call
// reaches the API with the bearer, and the result comes back as text.
func TestRegisterAPITools_ListAndCallOverMCP(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"mode":"dryrun"}`))
	}))
	defer srv.Close()

	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerAPITools(s, &configuration.Configuration{MCPAPIURL: srv.URL, APIToken: "tok"}, map[string]bool{"list_signals": true})

	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := s.Connect(ctx, st, nil)
	require.NoError(t, err)
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	require.NoError(t, err)
	defer cs.Close()

	list, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, tl := range list.Tools {
		names[tl.Name] = true
	}
	assert.True(t, names["get_settings"])
	assert.True(t, names["pause_candle_dataset"])
	assert.False(t, names["list_signals"], "a name taken by an agent tool is skipped")
	assert.Len(t, names, len(apiTools)-1)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_settings", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Contains(t, res.Content[0].(*mcp.TextContent).Text, "dryrun")
}
