package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"go-trade-bot/app/entities"
)

// ReportReader reads agent reports (app/usecase/agentplatform.UseCase).
type ReportReader interface {
	GetReport(ctx context.Context, id uint) (entities.AgentReport, error)
}

// BacktestReader reads backtest runs (app/usecase/backtest.BacktestUseCase).
type BacktestReader interface {
	GetByID(ctx context.Context, id uint) (entities.BacktestRun, error)
}

// reportLinks builds browser URLs under the dashboard's public base URL.
// The pages are served by cmd/api behind the same login as the dashboard, so
// a link works in any browser (built-in or native) where the operator is
// signed in; no credentials are ever put in the URL.
type reportLinks struct{ base string }

func (l reportLinks) page(path string) string {
	return strings.TrimRight(l.base, "/") + path
}

func reportTools(base string, reports ReportReader, backtests BacktestReader) []Tool {
	l := reportLinks{base: base}
	var tools []Tool
	if reports != nil {
		tools = append(tools, Tool{
			Name:        "get_agent_report_url",
			Description: "Browser links for an existing agent report (from list_reports or write_report): `url` opens it in the dashboard, `html_url` is the raw standalone HTML document. Open them in a browser where you are signed in to the dashboard.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"report_id":{"type":"integer"}},"required":["report_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					ReportID uint `json:"report_id"`
				}
				if err := decode("get_agent_report_url", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("get_agent_report_url", "report_id", in.ReportID); err != nil {
					return nil, err
				}
				rep, err := reports.GetReport(ctx, in.ReportID)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"report_id": rep.ID, "title": rep.Title,
					"url":      l.page(fmt.Sprintf("/agents/reports/%d", rep.ID)),
					"html_url": l.page(fmt.Sprintf("/api/agent-reports/%d/html", rep.ID)),
				}, nil
			},
		})
	}
	if backtests != nil {
		tools = append(tools, Tool{
			Name:        "get_backtest_report_url",
			Description: "Browser link to the standalone HTML report of a finished backtest run (metrics, equity curve, trade table). The server renders it on demand from the stored result, so it works even after the report file was pruned. lang is optional: en (default), es or pt-BR. Open it in a browser where you are signed in to the dashboard.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"backtest_id":{"type":"integer"},"lang":{"type":"string","enum":["en","es","pt-BR"]}},"required":["backtest_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					BacktestID uint   `json:"backtest_id"`
					Lang       string `json:"lang"`
				}
				if err := decode("get_backtest_report_url", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("get_backtest_report_url", "backtest_id", in.BacktestID); err != nil {
					return nil, err
				}
				run, err := backtests.GetByID(ctx, in.BacktestID)
				if err != nil {
					return nil, err
				}
				if run.Status != "" && run.Status != entities.BacktestDone {
					return nil, fmt.Errorf("get_backtest_report_url: backtest %d is %s - the report exists once it is done (check get_backtest)", run.ID, run.Status)
				}
				u := l.page(fmt.Sprintf("/api/backtest/%d/report", run.ID))
				if in.Lang != "" {
					u += "?lang=" + url.QueryEscape(in.Lang)
				}
				return map[string]any{"backtest_id": run.ID, "url": u}, nil
			},
		})
	}
	return tools
}
