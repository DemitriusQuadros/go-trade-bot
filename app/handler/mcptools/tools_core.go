package mcptools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"go-trade-bot/app/entities"
)

const emptyObject = `{"type":"object","properties":{}}`

// --- settings ------------------------------------------------------------------

// settingsView is deliberately narrower than the REST settings DTO: broker
// keys (even masked), webhook URL and API token are never shown to an MCP
// client, and neither are display-only monitoring links.
type settingsView struct {
	Mode                   string  `json:"mode"`
	Testnet                bool    `json:"testnet"`
	DryRunSlippagePct      float64 `json:"dry_run_slippage_pct"`
	DryRunFeePct           float64 `json:"dry_run_fee_pct"`
	DryRunFillDelayMs      int     `json:"dry_run_fill_delay_ms"`
	AgentsPaused           bool    `json:"agents_paused"`
	DefaultLocale          string  `json:"default_locale"`
	BacktestTimeoutMinutes int     `json:"backtest_timeout_minutes"`
}

func settingsTools(r SettingsReader) []Tool {
	return []Tool{{
		Name:        "get_settings",
		Description: "Platform settings that matter for reasoning about risk: the execution-mode ceiling (dryrun/paper/live), testnet, dry-run fee/slippage, the agents kill switch, default locale and backtest timeout. Credentials and URLs are never included.",
		InputSchema: json.RawMessage(emptyObject),
		Run: func(ctx context.Context, _ json.RawMessage) (any, error) {
			s, err := r.Get(ctx)
			if err != nil {
				return nil, err
			}
			locale := s.DefaultLocale
			if locale == "" {
				locale = "en"
			}
			return settingsView{
				Mode: s.Mode, Testnet: s.Testnet,
				DryRunSlippagePct: s.DryRunSlippagePct, DryRunFeePct: s.DryRunFeePct, DryRunFillDelayMs: s.DryRunFillDelayMs,
				AgentsPaused: s.AgentsPaused, DefaultLocale: locale,
				BacktestTimeoutMinutes: int(s.BacktestTimeout() / time.Minute),
			}, nil
		},
	}}
}

// --- accounts ------------------------------------------------------------------

func accountTools(r AccountReader) []Tool {
	return []Tool{{
		Name:        "get_account",
		Description: "Balances and order slots per account mode (dryrun, live): amount (free balance, the ceiling a script's buy.qty is clamped to), initial/locked amount, max allocation, available order slots, currency, last exchange sync.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","description":"optional - only this account mode (dryrun|live|paper)"}}}`),
		Run: func(_ context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				Mode string `json:"mode"`
			}
			if err := decode("get_account", raw, &in); err != nil {
				return nil, err
			}
			all, err := r.GetAllAccounts()
			if err != nil {
				return nil, err
			}
			out := make([]entities.Account, 0, len(all))
			for _, a := range all {
				if in.Mode == "" || string(a.Mode) == in.Mode {
					out = append(out, a)
				}
			}
			return map[string]any{"accounts": out}, nil
		},
	}}
}

// --- signals -------------------------------------------------------------------

type orderView struct {
	ID              uint    `json:"id"`
	EntryPrice      float32 `json:"entry_price"`
	ExitPrice       float32 `json:"exit_price"`
	Quantity        float32 `json:"quantity"`
	InvestedAmount  float32 `json:"invested_amount"`
	StopLossPrice   float32 `json:"stop_loss_price,omitempty"`
	TakeProfitPrice float32 `json:"take_profit_price,omitempty"`
	EntryFee        float32 `json:"entry_fee"`
	ExitFee         float32 `json:"exit_fee"`
	Profit          float32 `json:"profit"`
	ExitReason      string  `json:"exit_reason,omitempty"`
	Simulated       bool    `json:"simulated"`
	OpenedAt        string  `json:"opened_at"`
	ClosedAt        string  `json:"closed_at,omitempty"`
}

type signalView struct {
	ID         uint        `json:"id"`
	Symbol     string      `json:"symbol"`
	StrategyID uint        `json:"strategy_id"`
	Status     string      `json:"status"`
	Mode       string      `json:"mode,omitempty"`
	OpenedAt   string      `json:"opened_at"`
	UpdatedAt  string      `json:"updated_at"`
	Orders     []orderView `json:"orders"`
}

func toSignalView(s entities.Signal) signalView {
	v := signalView{
		ID: s.ID, Symbol: s.Symbol, StrategyID: s.StrategyID, Status: string(s.Status), Mode: s.Mode,
		OpenedAt: ts(s.CreatedAt), UpdatedAt: ts(s.UpdatedAt), Orders: make([]orderView, 0, len(s.Orders)),
	}
	for _, o := range s.Orders {
		ov := orderView{
			ID: o.ID, EntryPrice: o.EntryPrice, ExitPrice: o.ExitPrice, Quantity: o.Quantity, InvestedAmount: o.InvestedAmount,
			StopLossPrice: o.StopLossPrice, TakeProfitPrice: o.TakeProfitPrice, EntryFee: o.EntryFee, ExitFee: o.ExitFee,
			Profit: o.Profit, ExitReason: o.ExitReason, Simulated: strings.HasPrefix(o.BrokerOrderID, "SIM-"),
			OpenedAt: ts(o.CreatedAt),
		}
		if o.IsClosing {
			ov.ClosedAt = ts(o.UpdatedAt)
		}
		v.Orders = append(v.Orders, ov)
	}
	return v
}

func signalTools(r SignalReader) []Tool {
	return []Tool{
		{
			Name:        "list_signals",
			Description: "Trade signals (positions) with their orders, newest first. Unlike get_open_positions this includes closed history with realized profit and exit reason. Filter by status and strategy_id; limit defaults to 50 (max 200).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["open","closed","all"],"description":"default all"},"strategy_id":{"type":"integer"},"limit":{"type":"integer","description":"default 50, max 200"}}}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					Status     string `json:"status"`
					StrategyID uint   `json:"strategy_id"`
					Limit      int    `json:"limit"`
				}
				if err := decode("list_signals", raw, &in); err != nil {
					return nil, err
				}
				var (
					all []entities.Signal
					err error
				)
				switch in.Status {
				case "", "all":
					all, err = r.GetAll(ctx)
				case "open":
					all, err = r.GetAllOpen(ctx)
				case "closed":
					all, err = r.GetAllClosed(ctx)
				default:
					return nil, errBadStatus
				}
				if err != nil {
					return nil, err
				}
				sort.SliceStable(all, func(i, j int) bool { return all[i].ID > all[j].ID })
				limit := clamp(in.Limit, 50, 200)
				out := make([]signalView, 0, limit)
				for _, s := range all {
					if in.StrategyID != 0 && s.StrategyID != in.StrategyID {
						continue
					}
					out = append(out, toSignalView(s))
					if len(out) == limit {
						break
					}
				}
				return map[string]any{"signals": out, "returned": len(out), "limit": limit}, nil
			},
		},
		{
			Name:        "get_signal",
			Description: "One signal (position) with its orders, by id.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"signal_id":{"type":"integer"}},"required":["signal_id"]}`),
			Run: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var in struct {
					SignalID uint `json:"signal_id"`
				}
				if err := decode("get_signal", raw, &in); err != nil {
					return nil, err
				}
				if err := requireID("get_signal", "signal_id", in.SignalID); err != nil {
					return nil, err
				}
				s, err := r.GetByID(ctx, in.SignalID)
				if err != nil {
					return nil, err
				}
				return toSignalView(s), nil
			},
		},
	}
}
