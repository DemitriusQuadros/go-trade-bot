package real

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-trade-bot/app/entities"

	"github.com/cucumber/godog"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// state is the per-scenario world plus named values remembered between steps.
type state struct {
	w    *world
	vars map[string]string
}

func (s *state) subst(in string) string {
	out := strings.ReplaceAll(in, "{id}", strconv.FormatUint(uint64(s.w.lastID), 10))
	for k, v := range s.vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// jsonPath resolves "a.b.0.c" in decoded JSON.
func jsonPath(raw []byte, path string) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("not JSON (%v): %.200s", err, raw)
	}
	for _, part := range strings.Split(path, ".") {
		switch t := v.(type) {
		case map[string]any:
			next, ok := t[part]
			if !ok {
				return nil, fmt.Errorf("no key %q at %q in %.300s", part, path, raw)
			}
			v = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(t) {
				return nil, fmt.Errorf("bad index %q at %q", part, path)
			}
			v = t[i]
		default:
			return nil, fmt.Errorf("cannot descend into %T at %q", v, part)
		}
	}
	return v, nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

func register(sc *godog.ScenarioContext) {
	s := &state{vars: map[string]string{}}

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		w, err := newWorld()
		if err != nil {
			return ctx, err
		}
		s.w, s.vars = w, map[string]string{}
		return ctx, nil
	})
	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		if s.w != nil {
			s.w.close()
		}
		return ctx, nil
	})

	// ---- fixtures ----
	sc.Step(`^a script strategy "Trader" exists$`, func() error { return s.w.seedStrategy() })
	sc.Step(`^(\d+) days of BTCUSDT 1h candles exist from "([^"]*)"$`, func(days int, from string) error {
		t, err := time.Parse("2006-01-02", from)
		if err != nil {
			return err
		}
		return s.w.seedCandles(t, days)
	})
	sc.Step(`^settings with broker credentials and a webhook URL are stored$`, func() error {
		return s.w.db.Create(&entities.Settings{
			ID: 1, Mode: "dryrun", BrokerApiKey: "AKIA-LIVE-KEY-1234", BrokerApiSecret: "LIVE-SECRET-VALUE-5678",
			BrokerTestnetApiKey: "TESTNET-KEY-9999", BrokerTestnetApiSecret: "TESTNET-SECRET-0000",
			WebhookURL: "https://hooks.example.com/services/T000/B000/XXXX", APIToken: "bearer-token-abc",
		}).Error
	})
	sc.Step(`^the MCP client is connected$`, func() error { return s.w.startMCP(context.Background()) })
	sc.Step(`^the task queue is (down|up)$`, func(state string) error {
		s.w.queue.mu.Lock()
		s.w.queue.down = state == "down"
		s.w.queue.mu.Unlock()
		return nil
	})
	sc.Step(`^the worker processes the queue$`, func() error { return s.w.runWorker(context.Background()) })

	// ---- REST ----
	do := func(method, path string, body []byte) error {
		req, err := http.NewRequest(method, s.w.api.URL+s.subst(path), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		s.w.status, s.w.header = resp.StatusCode, resp.Header
		s.w.body, _ = io.ReadAll(resp.Body)
		return nil
	}
	sc.Step(`^I POST "([^"]*)" with JSON:$`, func(path string, doc *godog.DocString) error {
		return do(http.MethodPost, path, []byte(s.subst(doc.Content)))
	})
	sc.Step(`^I GET "([^"]*)"$`, func(path string) error { return do(http.MethodGet, path, nil) })
	sc.Step(`^the response status is (\d+)$`, func(code int) error {
		if s.w.status != code {
			return fmt.Errorf("status = %d, want %d; body: %.400s", s.w.status, code, s.w.body)
		}
		return nil
	})
	sc.Step(`^the response header "([^"]*)" is "([^"]*)"$`, func(name, want string) error {
		if got := s.w.header.Get(name); got != s.subst(want) {
			return fmt.Errorf("header %s = %q, want %q", name, got, s.subst(want))
		}
		return nil
	})
	sc.Step(`^the response JSON "([^"]*)" is "([^"]*)"$`, func(path, want string) error {
		v, err := jsonPath(s.w.body, path)
		if err != nil {
			return err
		}
		if got := asString(v); got != s.subst(want) {
			return fmt.Errorf("%s = %q, want %q", path, got, s.subst(want))
		}
		return nil
	})
	sc.Step(`^the response body contains "([^"]*)"$`, func(sub string) error {
		if !contains(string(s.w.body), s.subst(sub)) {
			return fmt.Errorf("body does not contain %q: %.400s", sub, s.w.body)
		}
		return nil
	})
	sc.Step(`^I remember the response id$`, func() error {
		v, err := jsonPath(s.w.body, "id")
		if err != nil {
			return err
		}
		id, err := strconv.ParseUint(asString(v), 10, 32)
		s.w.lastID = uint(id)
		return err
	})
	sc.Step(`^(\d+) backtest tasks? (?:is|are) queued$`, func(n int) error {
		s.w.queue.mu.Lock()
		defer s.w.queue.mu.Unlock()
		if len(s.w.queue.backtests) != n {
			return fmt.Errorf("%d backtest tasks queued, want %d", len(s.w.queue.backtests), n)
		}
		return nil
	})
	sc.Step(`^there (?:is|are) (\d+) backtest runs? stored$`, func(n int) error {
		var count int64
		s.w.db.Model(&entities.BacktestRun{}).Count(&count)
		if int(count) != n {
			return fmt.Errorf("%d backtest runs stored, want %d", count, n)
		}
		return nil
	})
	sc.Step(`^the only stored backtest run has status "([^"]*)"$`, func(want string) error {
		var runs []entities.BacktestRun
		s.w.db.Find(&runs)
		if len(runs) != 1 {
			return fmt.Errorf("%d runs stored, want exactly 1", len(runs))
		}
		if string(runs[0].Status) != want {
			return fmt.Errorf("status = %q (error %q), want %q", runs[0].Status, runs[0].ErrorMessage, want)
		}
		return nil
	})
	sc.Step(`^the stored report files are deleted$`, func() error {
		files, _ := filepath.Glob(filepath.Join(s.w.reportsDir, "*"))
		if len(files) == 0 {
			return fmt.Errorf("no report file was written to %s - nothing to delete", s.w.reportsDir)
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				return err
			}
		}
		return nil
	})

	// ---- MCP ----
	call := func(name string, args map[string]any) error {
		res, err := s.w.mcp.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			s.w.lastMCP, s.w.lastErr = err.Error(), true
			return nil
		}
		var b strings.Builder
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				b.WriteString(t.Text)
			}
		}
		s.w.lastMCP, s.w.lastErr = b.String(), res.IsError
		return nil
	}
	sc.Step(`^I call MCP tool "([^"]*)" with:$`, func(name string, doc *godog.DocString) error {
		args := map[string]any{}
		if err := json.Unmarshal([]byte(s.subst(doc.Content)), &args); err != nil {
			return fmt.Errorf("bad step JSON: %w", err)
		}
		return call(name, args)
	})
	sc.Step(`^I call MCP tool "([^"]*)"$`, func(name string) error { return call(name, map[string]any{}) })
	sc.Step(`^the MCP call succeeds$`, func() error {
		if s.w.lastErr {
			return fmt.Errorf("MCP call failed: %.400s", s.w.lastMCP)
		}
		return nil
	})
	sc.Step(`^the MCP call fails$`, func() error {
		if !s.w.lastErr {
			return fmt.Errorf("MCP call unexpectedly succeeded: %.400s", s.w.lastMCP)
		}
		return nil
	})
	sc.Step(`^the MCP result contains "([^"]*)"$`, func(sub string) error {
		if !contains(s.w.lastMCP, s.subst(sub)) {
			return fmt.Errorf("MCP result does not contain %q: %.600s", sub, s.w.lastMCP)
		}
		return nil
	})
	sc.Step(`^the MCP result does not contain "([^"]*)"$`, func(sub string) error {
		if contains(s.w.lastMCP, s.subst(sub)) {
			return fmt.Errorf("MCP result must not contain %q: %.600s", sub, s.w.lastMCP)
		}
		return nil
	})
	sc.Step(`^the MCP result JSON "([^"]*)" is "([^"]*)"$`, func(path, want string) error {
		v, err := jsonPath([]byte(s.w.lastMCP), path)
		if err != nil {
			return err
		}
		if got := asString(v); got != s.subst(want) {
			return fmt.Errorf("%s = %q, want %q", path, got, s.subst(want))
		}
		return nil
	})
	sc.Step(`^I remember MCP result JSON "([^"]*)" as "([^"]*)"$`, func(path, name string) error {
		v, err := jsonPath([]byte(s.w.lastMCP), path)
		if err != nil {
			return err
		}
		s.vars[name] = asString(v)
		return nil
	})
	sc.Step(`^I remember MCP result matching "([^"]*)" as "([^"]*)"$`, func(pattern, name string) error {
		m := regexp.MustCompile(pattern).FindStringSubmatch(s.w.lastMCP)
		if len(m) < 2 {
			return fmt.Errorf("MCP result does not match %q: %.400s", pattern, s.w.lastMCP)
		}
		s.vars[name] = m[1]
		return nil
	})
	sc.Step(`^I GET the URL in MCP result JSON "([^"]*)"$`, func(path string) error {
		v, err := jsonPath([]byte(s.w.lastMCP), path)
		if err != nil {
			return err
		}
		resp, err := http.Get(asString(v))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		s.w.status, s.w.header = resp.StatusCode, resp.Header
		s.w.body, _ = io.ReadAll(resp.Body)
		return nil
	})
	listTools := func() ([]string, error) {
		res, err := s.w.mcp.ListTools(context.Background(), nil)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(res.Tools))
		for _, t := range res.Tools {
			names = append(names, t.Name)
		}
		return names, nil
	}
	sc.Step(`^the MCP tool list contains "([^"]*)"$`, func(name string) error {
		names, err := listTools()
		if err != nil {
			return err
		}
		for _, n := range names {
			if n == name {
				return nil
			}
		}
		return fmt.Errorf("tool %q not listed; have %v", name, names)
	})
	sc.Step(`^the MCP tool list has no tool matching "([^"]*)"$`, func(pattern string) error {
		names, err := listTools()
		if err != nil {
			return err
		}
		re := regexp.MustCompile(pattern)
		for _, n := range names {
			if re.MatchString(n) {
				return fmt.Errorf("tool %q must not be exposed over MCP", n)
			}
		}
		return nil
	})
	sc.Step(`^an agent run was enqueued for agent "([^"]*)"$`, func(id string) error {
		s.w.queue.mu.Lock()
		defer s.w.queue.mu.Unlock()
		for _, r := range s.w.queue.agentRuns {
			if strconv.FormatUint(uint64(r.AgentID), 10) == s.subst(id) {
				return nil
			}
		}
		return fmt.Errorf("no agent run enqueued for agent %s (have %d runs)", s.subst(id), len(s.w.queue.agentRuns))
	})
	sc.Step(`^no agent run was enqueued$`, func() error {
		s.w.queue.mu.Lock()
		defer s.w.queue.mu.Unlock()
		if len(s.w.queue.agentRuns) != 0 {
			return fmt.Errorf("%d agent runs were enqueued", len(s.w.queue.agentRuns))
		}
		return nil
	})
}
