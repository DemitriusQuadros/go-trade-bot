# AGENTS.md

How this project is actually developed, for any AI coding agent working in this repo (tool-agnostic — see
`CLAUDE.md` for Claude Code-specific technical reference and current architecture).

## The core pattern: spec-driven, agent-delegated development

Almost none of this codebase was written by a single agent working directly. The pattern, repeated across
four backend phases and one major frontend pivot:

1. **A `software-architect` agent** turns product requirements into a technical blueprint and, closer to
   implementation time, into detailed per-capability specs (Overview → Current Behavior with file:line
   references → Target Behavior with real signatures → numbered Given/When/Then Acceptance Criteria → Out
   of Scope → Dependencies). Specs are written to be implementable by a different agent with no
   conversation history — they must be self-contained.
2. **`go-backend-dev`/`frontend-specialist` agents** implement against those specs, usually in the
   background, often running in parallel when the backend and frontend halves of a feature are independent
   enough (they aren't fully independent — see "Cross-agent contracts" below).
3. **A coordinator (you, reading this)** does not trust either side's specs or implementation reports at
   face value. Every non-trivial claim in an agent's final report in this project's history has been
   spot-checked with a direct `grep`/`read` against the actual code before being treated as true. This has
   repeatedly caught real divergence: specs assuming an API shape that wasn't what got built, agents
   reporting a fix that only covered part of the actual bug, deviations from frozen interfaces, and outright
   contradicted decisions (see "Known current inconsistency" below).

**Do this too.** If you're picking up work in this repo, don't assume `docs/specs/**/*.md` describes the
code as it exists today — verify. Don't assume an agent's own final-report summary is complete — check the
actual diff.

## Where things live

```
docs/prd/                        Product requirements (the "why")
docs/architecture/                Blueprints — system-level design + ADRs (the "how, broadly")
  refactoring-blueprint.md          Backend Phases 1-4
  web-frontend-blueprint.md         The web frontend pivot (after the TUI was deleted)
docs/specs/phase-{1,2,3,4}/        Per-phase backend implementation specs
docs/specs/web-frontend/           Web frontend implementation specs (backend-*.md + frontend-*.md)
.agents/workflows/*.md            Agent role definitions (software-architect, go-backend-dev,
                                   frontend-specialist, qa-specialist, product-manager-prd)
```

Read in that order when picking up unfamiliar work: PRD for why, blueprint for the big picture and ADRs
(architecture decisions with their rationale — don't relitigate a decision without reading why it was made),
specs for the exact contract, then the actual code to confirm the specs still hold.

## Project history at a glance

1. **Phase 1** (live trading core, strategy plugin system) → **Phase 2** (backtesting, candle storage,
   observability) → **Phase 3** (multi-timeframe, position sizing, a full TUI built on `termui/v3`) →
   **Phase 4** (hyperparameter optimization, Monte Carlo, gRPC ML strategy adapter).
2. After Phase 3/4's TUI shipped and got real hands-on use, it was judged too limited — wrong medium
   entirely for how the bot is actually operated — and **removed outright** (not kept as a fallback, which
   was the first recommendation; overridden by explicit user decision). See `docs/prd/refactoring.md` §10's
   "Reversed decision" note and `web-frontend-blueprint.md` §8/ADR-011 for the full reasoning and the
   superseded original recommendation, kept for record rather than deleted.
3. A **web frontend** (React/TS/Vite, embedded into `cmd/api` via `go:embed`) was specced and built to
   replace it, reusing the entire existing REST API with zero backend changes for feature parity — one new
   requirement (bearer-token auth) was added deliberately, since a browser-reachable trading control plane
   has a materially different threat model than a TUI that requires shell access.

All of this lives on branch `refactor/full-rewrite`, tracked as one long-lived umbrella PR rather than
merged phase-by-phase.

## Cross-agent contracts — the recurring failure mode to watch for

When backend and frontend specs/implementation are split across two agents working concurrently, the
seam between them is where things break:
- Query parameter names, response field casing, and error shapes get guessed by whichever side writes
  first, then need reconciliation once the other side's real contract lands.
- The right process (used repeatedly in this project): each side flags its guesses explicitly
  ("reconcile against `backend-0X` once it exists"), and a reconciliation pass happens once both land —
  verified directly, not just re-summarized.
- Real examples from this project's history: the P&L history endpoint's query param name changed from a
  guessed `granularity=` to the actual `bucket=` (plus a required `symbol=` param neither side had
  originally scoped); `entities.Strategy`/`Signal` serializing as PascalCase JSON was caught only because
  someone checked the raw handler code, not the spec.

## Known safety-critical invariants — do not casually change these

This bot executes real trades with real money. Several deliberate, non-obvious safety decisions exist —
see `CLAUDE.md`'s "Safety notes" section for the mechanics. If a task seems to require relaxing one of
them (the dual-layer `Mode`/`MODE` guard, `CONFIRM_LIVE`, Testnet/Mode consistency, real exchange-side
stop-loss instead of software polling), treat that as a signal to stop and confirm with the user rather
than a normal refactor.

## Known outstanding issues (as of the last full scope audit)

- **Frontend stack inconsistency**: a later "redesign" commit introduced React Query, Tailwind/Shadcn, and
  `lightweight-charts` without reconciling against the original specs (which deliberately chose Recharts
  and explicitly rejected React Query for this project's scale). `package.json` currently has both charting
  libraries installed and components split between them. Needs a real decision and cleanup, not a silent
  continuation in either direction.
- **Repo hygiene**: over a dozen one-off patch scripts (`fix_dashboard*.js`, `refactor*.{js,py}`,
  `rewrite.js`, `cleanup.js`, `fix_opt*.js`, `fix.py`) from that same redesign work got committed to the
  repo root. They aren't part of the application and should be removed.
- **`config.example.yml` is stale** — missing every config key added since the original 3-service setup
  (`MODE`, `CONFIRM_LIVE`, `TESTNET`, `WEBHOOK_URL`, `DRY_RUN.*`, `API_TOKEN`). Check
  `internal/configuration/configuration.go` for the real current list.
- **No frontend test suite** exists yet, unlike the backend's phase-by-phase E2E coverage.
