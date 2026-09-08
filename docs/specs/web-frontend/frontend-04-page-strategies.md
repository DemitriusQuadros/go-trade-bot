# Spec frontend-04 — Strategies Page (`web/src/pages/Strategies.tsx`)

## Overview

Web equivalent of the deleted TUI's Page 2 (`docs/specs/phase-3/tui-03-page-strategies.md`), split across
two phases in this single spec (per this task's instruction) — sections and Acceptance Criteria below are
each explicitly labeled **[Phase B]** (list/detail, read-only) or **[Phase C]** (create/edit form, status/mode
actions). Also absorbs the P&L history sparkline addition from the deleted TUI's Phase 4 spec
(`docs/specs/phase-4/tui-02-pnl-history-sparkline.md`) into the detail view, as that spec did for its own
overlay.

## Current Behavior

- No web strategies page exists. Feature-parity reference: `tui-03-page-strategies.md` (table + detail
  overlay + `[e]`/`[d]`/`[b]`/`[r]` actions) and `tui-02-pnl-history-sparkline.md` (P&L history addition to
  the same detail view).
- `GET /strategy` returns the raw `entities.Strategy[]` (verified, `frontend-01`'s Judgment Call #1 on
  casing applies here). `GET /strategy/performance` returns **every** `(strategy, symbol)` performance row
  in one call with no filter param (per the deleted TUI's `tui-01` corrected Target Behavior) — the web
  client filters this client-side by `StrategyName`/`ID`, same as the TUI did.
- `PATCH /strategy/{id}/status` (`{"status": "productive"|"testing"|"disabled"}`) and
  `PATCH /strategy/{id}/mode` (`{"mode": "backtest"|"dryrun"|"paper"|"live"}`) both persist immediately but
  a mode change only takes **effect** at the strategy's next scheduled cycle (`app/handler/tasks/strategy`'s
  `gateMode` re-reads `Strategy.Mode` fresh every cycle) — this is a real backend behavior, not a TUI-only
  quirk, and the web UI must represent the same "saved vs. active" distinction the TUI's `tui-03` AC#4a
  introduced.
- `GET /strategy/{id}/performance/history?symbol=&bucket=daily|weekly|monthly&limit=` (backend-05, final) —
  `symbol` required, rows are sparse (no zero-profit row for empty buckets).
- No `PUT /strategy/{id}` full-object edit form exists in any prior client — the deleted TUI never exposed
  one (its detail overlay was explicitly read-only, `tui-03`'s Out of Scope). This spec's Phase C create/edit
  form is genuinely new UI, not a web port of an existing TUI capability — a deliberate scope expansion the
  blueprint's §7 table calls out ("a real syntax-highlighted, editable JSON config panel in a modal... vs.
  terminal preview-only").

## Target Behavior

### Layout — component composition

```
Strategies.tsx
├── PageHeader — "Strategies", [Phase C] "+ New Strategy" button (opens StrategyFormModal)
├── Toolbar [Phase B: sort/filter] — text search (name/symbol), status filter, mode filter,
│    sort-by-column click on table headers (a materially richer control set than the TUI's
│    fixed column order, per blueprint §7)
├── StrategyTable (frontend-10, full variant)
│    columns: Name | Algorithm (StrategyName) | Symbols | Cycle | Mode (ModeBadge) | Last Exec |
│             Status | Open Signals | [Phase C] row actions
│    — [Phase B] row click → expands/navigates to StrategyDetailPanel (below)
│    — [Phase C] per-row action menu: Enable/Disable (PATCH status), Cycle Mode (PATCH mode),
│                Edit (opens StrategyFormModal pre-filled), Run Backtest (navigates to
│                /backtest?strategy_id=<id>, frontend-06's pre-selection)
│    — [Phase C] multi-select checkboxes + bulk "Enable selected"/"Disable selected" toolbar
│                action (blueprint §7: "multi-select bulk enable/disable" — a materially richer
│                control than the TUI's single-row-at-a-time [e]/[d])
└── StrategyDetailPanel (opens as a modal/drawer, not a full-page nav — a "detail overlay" in
     spirit, rendered as a proper focus-trapped dialog instead of a terminal panel)
     ├── [Phase B] Config JSON — syntax-highlighted, read-only <pre>/code block (Phase B) —
     │    [Phase C] becomes an editable, syntax-highlighted JSON textarea/editor (e.g. CodeMirror
     │    or a plain <textarea> with client-side JSON.parse validation — exact library choice
     │    left open per the blueprint's own "genuinely low-stakes" deferral)
     ├── [Phase B] Lifetime P&L / total trades / win rate (from GET /strategy/performance,
     │    filtered client-side by this strategy's Name — same source/filter the TUI used)
     └── [Phase B] P&L History chart (PnlHistoryChart, frontend-11) with a bucket toggle
          (daily/weekly/monthly, a real segmented control instead of a [g] keypress) and, only
          if MonitoredSymbols.length > 1, a symbol <select> (instead of the TUI's [y] keypress
          cycle) — blueprint §7: "a full interactive chart with a daily/weekly/monthly toggle
          and multi-strategy overlay/comparison," the latter being this page's own further
          enhancement beyond the TUI's single-sparkline scope (see Judgment Call).
```

### `StrategyFormModal` [Phase C] — create/edit

```tsx
// web/src/pages/Strategies.tsx (sketch, Phase C addition)
interface StrategyFormState {
  name: string;
  description: string;
  strategy_name: string;       // dropdown, sourced from the set of StrategyName values already
                                 // seen across GET /strategy's response — NOT a registry-listing
                                 // endpoint (none exists; app/strategies/registry.go is server-side
                                 // only, matching the deleted TUI's tui-05 same non-goal)
  monitored_symbols: string[]; // tag input, free text (no exchange-symbol validation, matching
                                 // tui-05's Out of Scope carried forward)
  status: 'productive' | 'testing' | 'disabled';
  mode: 'backtest' | 'dryrun' | 'paper' | 'live';
  cycle: number; // minutes
  configuration: string; // raw JSON text, validated client-side via JSON.parse before submit
}

async function handleSubmit(form: StrategyFormState, editingId: number | null) {
  const body = {
    name: form.name,
    description: form.description,
    strategy_name: form.strategy_name,
    monitored_symbols: form.monitored_symbols,
    status: form.status,
    mode: form.mode,
    cycle: form.cycle,
    configuration: JSON.parse(form.configuration), // throws -> caught, shown as a field-level error
  };
  if (editingId) {
    await api.put(`/strategy/${editingId}`, body); // full-object PUT, matching existing endpoint
  } else {
    await api.post('/strategy', body);
  }
}
```

### Mode-switch "pending" indicator [Phase C]

Carrying forward the deleted TUI's `tui-03` AC#4a distinction: after `PATCH /strategy/{id}/mode` succeeds,
the `ModeBadge` (frontend-10) shows the new value immediately (optimistic update) but with a distinguishing
visual marker — this spec proposes a small "(pending — next cycle)" caption under the badge, cleared once
the next `GET /strategy` poll shows `LastExecutionTime` advance past the moment the `PATCH` was sent. This is
the same underlying mechanic as the TUI's `"PAPER*"` marker, expressed as a caption instead of an asterisk
since a browser has room for an actual sentence.

## Acceptance Criteria

### [Phase B]

1. **Given** `GET /strategy` returns N strategies, **when** the page renders, **then** the table shows
   exactly N rows with all eight parity columns (name, algorithm, symbols, cycle, mode, last execution,
   status, open signals count) — matching the deleted TUI's `tui-08` AC#1 column set.
2. **Given** the operator clicks a row, **when** `StrategyDetailPanel` opens, **then** it fetches
   `GET /strategy/performance` (filtered client-side to this strategy) and
   `GET /strategy/{id}/performance/history?symbol=<first symbol>&bucket=daily&limit=30` **concurrently**, and
   each section (config, lifetime P&L, history chart) shows its own independent loading state — mirrors the
   deleted TUI's Phase 4 `tui-02` AC#1/#2 (parallel fetch, independent per-section loading, not an
   all-or-nothing overlay spinner).
3. **Given** the history fetch returns an empty array (strategy too new to have completed buckets), **when**
   the chart section renders, **then** it shows "No history yet" rather than an empty/misleading flat-line
   chart — same distinction the deleted TUI's `tui-02` AC#4 drew.
4. **Given** the operator toggles the bucket segmented control (daily/weekly/monthly), **when** the toggle
   fires, **then** a fresh `GET /strategy/{id}/performance/history` call is issued with the new `bucket`
   value (server-side re-aggregation, never client-side re-bucketing — same rationale as the deleted TUI's
   `tui-02` Judgment Call #1) and the chart shows its own loading state during the refetch, discarding any
   stale in-flight response that resolves after a newer toggle (the same `(bucket, symbol)`-tagged
   stale-response guard as `tui-02` AC#8, implemented here via `usePolling`'s request-generation tracking or
   an `AbortController` per toggle).
5. **Given** a strategy monitors more than one symbol, **when** the detail panel renders, **then** a symbol
   `<select>` appears next to the bucket toggle, defaulting to `MonitoredSymbols[0]`; for a single-symbol
   strategy, the selector is omitted entirely (not rendered-but-disabled) — matching the TUI's `tui-02`
   AC#3a "no `[y]` hint at all" rule, translated to "no `<select>` element at all."
6. **Given** a strategy's `Configuration` is empty/null, **when** the config panel renders, **then** it shows
   "(no configuration)" rather than a raw `null`/empty-brace token — same edge case the TUI's `tui-03` AC#7
   established.
7. **Edge case — zero strategies**: **given** `GET /strategy` returns `[]`, **when** the page renders,
   **then** the table area shows "No strategies configured" plus, uniquely enabled by Phase C once it ships,
   a "+ New Strategy" call-to-action inline in that empty state (not just a static string, unlike the TUI's
   parity requirement from `tui-08` AC#5).

### [Phase C]

8. **Given** the selected strategy's `Status !== 'productive'`, **when** the operator chooses "Enable" from
   the row action menu, **then** `PATCH /strategy/{id}/status {status: "productive"}` fires, and on success
   the row's `Status` cell updates optimistically before the next poll — matching the deleted TUI's `tui-03`
   AC#2's instant-feedback requirement.
9. **Given** the optimistic update in Acceptance Criterion #8 fails (`ApiError`/`NetworkError`), **when** the
   promise rejects, **then** the row's displayed `Status` reverts to its last-known-good value and a toast
   (frontend-10's toast pattern, or an inline row-scoped error banner) surfaces the failure — never silently
   desyncing displayed state from server state, matching `tui-03` AC#3.
10. **Given** "Cycle Mode" is chosen on a strategy currently in `"live"`, **when** the action fires, **then**
    it calls `PATCH /strategy/{id}/mode {mode: "dryrun"}` — the cycle always wraps `live → dryrun`, never
    `live → live` or skipping a tier, matching `tui-03` AC#4's strict `dryrun → paper → live → dryrun` order.
11. **Given** a mode-switch PATCH succeeds, **when** the table re-renders, **then** the `ModeBadge` shows the
    new value with the "(pending — next cycle)" caption until the next `GET /strategy` poll shows
    `LastExecutionTime` has advanced past the PATCH's send time — Target Behavior's translation of `tui-03`
    AC#4a.
12. **Given** the operator selects multiple rows via checkboxes and clicks "Disable selected," **when** the
    bulk action fires, **then** one `PATCH /strategy/{id}/status` call is issued per selected row
    (sequentially or via `Promise.allSettled`, this spec's choice — see Judgment Call), and any individual
    failure is reported per-row (a bulk action partially succeeding must not silently report full success) —
    a capability the TUI never offered at all (single-row-only actions).
13. **Given** the operator opens "+ New Strategy," fills the form, and submits with a `Cycle` value and valid
    JSON in the configuration field, **when** the form submits, **then** `POST /strategy` is called with the
    constructed body and, on `201`, the modal closes and the table refetches to show the new row.
14. **Given** the configuration textarea contains invalid JSON at submit time, **when** `JSON.parse` throws,
    **then** the form shows a field-level error under the configuration field and does **not** call
    `POST`/`PUT /strategy` — client-side validation prevents a guaranteed-400 round-trip.
15. **Edge case — editing an existing strategy**: **given** "Edit" is chosen from a row's action menu,
    **when** `StrategyFormModal` opens, **then** every field (including `configuration`, pretty-printed via
    `JSON.stringify(cfg, null, 2)`) is pre-filled from that row's current values, and submitting calls
    `PUT /strategy/{id}` with the full object (not a partial patch) — matching the existing endpoint's
    full-replace semantics.
16. **Accessibility**: `StrategyFormModal` and `StrategyDetailPanel` are both real modal dialogs (focus
    trapped, `Escape` closes, focus returns to the triggering row's action button on close) — per
    `ConfirmDialog`'s shared pattern (frontend-10); the bulk-action checkboxes each have an accessible name
    tied to the strategy's `Name` (not just a bare unlabeled checkbox).

## Out of Scope

- Deleting a strategy — `DELETE /strategy/{id}` doesn't exist as a REST endpoint today (same absence the
  deleted TUI's `tui-03` Out of Scope confirmed); not added by this spec since it's a backend change outside
  this task's (docs-only) scope.
- Exchange-symbol autocomplete/validation for `monitored_symbols` — free-text tag input only, matching
  `tui-05`'s Out of Scope for the same reasoning (no live exchange-symbol-list lookup endpoint exists).
- A dedicated strategy-registry-listing endpoint for the `strategy_name` dropdown — this spec sources the
  dropdown's options from symbols already observed in `GET /strategy`'s response plus a small hardcoded
  fallback list (`grid`, `scalping`, `bollinger`, matching `app/strategies/*` package names) rather than
  inventing a new `GET /strategies/registry` endpoint (same non-goal the deleted TUI's `tui-05` stated).

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `api.get/post/put/patch`, `Strategy` type.
- `frontend-10-shared-components.md` — `StrategyTable`, `ModeBadge`, `ConfirmDialog` (for the enable/disable
  confirmation on `"live"`-mode strategies specifically — see Judgment Call), `usePolling`.
- `frontend-11-charts.md` — `PnlHistoryChart`.
- `docs/specs/phase-3/tui-03-page-strategies.md`, `docs/specs/phase-4/tui-02-pnl-history-sparkline.md` —
  feature/data-parity checklist only.
- `frontend-06-page-backtest-launcher.md` — the "Run Backtest" row action's navigation target and
  pre-selection contract.

## Judgment Calls (flagged for explicit reconciliation)

1. **Multi-strategy P&L overlay/comparison (blueprint §7's "compare strategies side by side on one chart")
   is named as a future enhancement to `StrategyDetailPanel`'s history chart, not built in this spec's v1.**
   The single-strategy daily/weekly/monthly toggle (direct parity with the deleted TUI) is what's specified
   above; a comparison view needs its own selection UI (which strategies to overlay) and is deferred to
   avoid scope-creeping this already-large spec — flagging since the blueprint's table implies it as an
   immediate web-version win, not a "someday" item.
2. **Bulk enable/disable issues one PATCH per row via `Promise.allSettled`, not a new batch endpoint.** No
   `PATCH /strategy/bulk-status` (or similar) exists; inventing one is backend work outside this docs-only
   task's scope. `Promise.allSettled` (rather than sequential awaiting) is chosen so N independent PATCHes
   don't serialize unnecessarily — flagging as a reasonable default given no backend batch primitive exists,
   not as a load-tested choice for very large N.
3. **A live-mode "are you sure" confirmation is proposed (via `ConfirmDialog`) before any mode-switch or
   enable action targeting `"live"`**, though neither the blueprint nor the deleted TUI spec mandates one —
   the TUI's `[r]` keypress had no confirmation step at all. This spec adds one specifically for the
   highest-stakes transition (a strategy about to start placing real orders) since a misclick in a mouse-driven
   UI is a materially easier accident than a deliberate terminal keypress sequence — flagged as this spec's
   own safety addition, confirm it's wanted before implementation, since it is new behavior beyond the TUI's
   parity bar.
