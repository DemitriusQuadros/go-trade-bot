# Spec frontend-10 — Shared Components (`web/src/components/*.tsx`, `web/src/hooks/*.ts`)

## Overview

The cross-page reusable pieces every `frontend-03` through `frontend-09` page spec depends on:
`StrategyTable`, `ModeBadge`, `PositionCard`, `ConfirmDialog`, and the `usePolling` hook. Written as a
dedicated spec since these are consumed by more than one page, matching the deleted TUI's own precedent
(`docs/specs/phase-3/tui-08-shared-components.md`) of pulling multi-page-consumed pieces into their own spec
rather than duplicating them per page.

## Current Behavior

- No shared web components exist. Feature-parity reference: `tui-08-shared-components.md` (`AccountSummary`,
  `StrategyTableCompact`/`StrategyTableFull`, `BuildSparkline`, `ComputeEMA20`, mode-badge color mapping) and
  `tui-04-page-positions.md` (position-card field set, P&L color/weight tiering).
- No polling/data-fetching hook exists — every page in this spec set assumes `usePolling` as its baseline
  data primitive (per ADR-008's "plain `useEffect` + polling," §2).

## Target Behavior

### `hooks/usePolling.ts`

```ts
// web/src/hooks/usePolling.ts
import { useEffect, useRef, useState, useCallback } from 'react';
import { NetworkError } from '@/api/client';

interface UsePollingOptions {
  intervalMs: number;
  enabled?: boolean; // default true — allows a page to pause polling (e.g. while a modal is open)
}

interface UsePollingResult<T> {
  data: T | null;       // last successfully fetched value — NEVER cleared on a failed poll
  error: Error | null;  // the most recent poll's error, if any (cleared on the next SUCCESSFUL poll)
  connected: boolean;   // false only while `error` is a NetworkError (server unreachable);
                         // an ApiError (4xx/5xx) does not flip this to false, since the server IS
                         // reachable, it just rejected the request — mirrors the deleted TUI's
                         // apiclient.ErrUnreachable vs. ErrAPI distinction (tui-01)
  loading: boolean;     // true only on the VERY FIRST fetch, never again on subsequent poll ticks
                         // (a poll tick failing/succeeding after the first load never re-shows a
                         // full-page spinner — only the degraded-mode `error` state changes)
  refetch: () => void;  // manual trigger, e.g. after a write action to refresh sooner than the
                         // next tick
}

export function usePolling<T>(
  fetcher: (signal: AbortSignal) => Promise<T>,
  { intervalMs, enabled = true }: UsePollingOptions,
): UsePollingResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const hasLoadedOnce = useRef(false);

  const tick = useCallback(
    async (signal: AbortSignal) => {
      try {
        const result = await fetcher(signal);
        if (signal.aborted) return;
        setData(result);
        setError(null);
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return;
        setError(err as Error);
      } finally {
        if (!hasLoadedOnce.current) {
          hasLoadedOnce.current = true;
          setLoading(false);
        }
      }
    },
    [fetcher],
  );

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    tick(controller.signal);
    const id = setInterval(() => tick(controller.signal), intervalMs);
    return () => {
      controller.abort();
      clearInterval(id);
    };
  }, [enabled, intervalMs, tick]);

  return {
    data,
    error,
    connected: !(error instanceof NetworkError),
    loading,
    refetch: () => tick(new AbortController().signal),
  };
}
```

### `components/StrategyTable.tsx`

```tsx
// web/src/components/StrategyTable.tsx
import type { Strategy } from '@/api/types';

interface StrategyTableProps {
  strategies: Strategy[];
  variant: 'compact' | 'full'; // compact: name + ModeBadge + status (Dashboard's right panel,
                                 // frontend-03); full: all 8 parity columns + row actions
                                 // (Strategies page, frontend-04)
  selectedId?: number;
  onRowClick?: (strategy: Strategy) => void;
  renderRowActions?: (strategy: Strategy) => React.ReactNode; // Phase C only, omitted in Phase B callers
}

export function StrategyTable({ strategies, variant, selectedId, onRowClick, renderRowActions }: StrategyTableProps) {
  if (strategies.length === 0) {
    return <p className="empty-state">No strategies configured.</p>; // matches tui-08 AC#5
  }
  // ...renders <table> with variant-dependent column set; each row is a real
  // <tr> with a <button>/<a> wrapping the clickable cell content for
  // onRowClick, never a bare div onClick (accessibility).
}
```

### `components/ModeBadge.tsx`

```tsx
// web/src/components/ModeBadge.tsx
import type { StrategyMode } from '@/api/types';

const MODE_STYLE: Record<StrategyMode, { label: string; className: string }> = {
  live:      { label: 'LIVE',  className: 'badge badge--live' },     // red — highest stakes
  paper:     { label: 'PAPER', className: 'badge badge--paper' },    // yellow
  dryrun:    { label: 'DRY',   className: 'badge badge--dryrun' },   // cyan
  backtest:  { label: 'BT',    className: 'badge badge--backtest' }, // blue
};
// Color mapping ported directly from the deleted TUI's tui-08 table — same
// red-for-LIVE "highest attention/risk" convention, carried forward rather
// than reinvented, since it's a proven, already-agreed-upon scheme.

interface ModeBadgeProps {
  mode: StrategyMode;
  pending?: boolean; // frontend-04's "(pending — next cycle)" caption, Acceptance Criterion there
}

export function ModeBadge({ mode, pending }: ModeBadgeProps) {
  const { label, className } = MODE_STYLE[mode];
  return (
    <span>
      <span className={className}>{label}</span>
      {pending && <span className="badge__pending-caption"> (pending — next cycle)</span>}
    </span>
  );
}
```

### `components/PositionCard.tsx`

```tsx
// web/src/components/PositionCard.tsx
import type { Signal } from '@/api/types';

interface PositionCardProps {
  signal: Signal;
  currentPrice: number | null; // null while price poll hasn't resolved yet / is in an error state
  priceStale?: boolean;         // true when the last successful price is being shown after a failed poll
  onClose?: (signalId: number) => void; // Phase C only; omitted renders no "Close Position" button
}

// P&L tiering — ported directly from the deleted TUI's tui-04 3-tier scheme:
function pnlTier(pnlPct: number): 'low' | 'mid' | 'high' {
  const abs = Math.abs(pnlPct);
  if (abs > 5) return 'high';
  if (abs >= 1) return 'mid';
  return 'low';
}
```

### `components/ConfirmDialog.tsx`

```tsx
// web/src/components/ConfirmDialog.tsx
interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string;   // default "Confirm"
  cancelLabel?: string;    // default "Cancel"
  destructive?: boolean;   // styles the confirm button red — used for position-close, strategy
                            // enable-to-live, etc.
  onConfirm: () => void;
  onCancel: () => void;
}

// Implementation notes (accessibility-critical, referenced by every page
// spec that uses this component):
// - Rendered via a native <dialog> element (or a well-tested modal library —
//   left open per the blueprint's low-stakes styling deferral) so focus-trap
//   and Escape-to-close come largely for free.
// - On open: focus moves to the dialog's heading or the cancel button (never
//   straight to the destructive confirm button, to avoid an accidental
//   Enter-key confirm).
// - On close (either button, or Escape): focus returns to the element that
//   triggered the dialog's open — every page spec's Acceptance Criteria
//   referencing "focus returns to the triggering button" depends on this.
```

### Connection-status badge (small, embedded pattern, not a standalone component per se)

Every page using `usePolling` derives its own connection badge from `usePolling`'s `connected` field —
this spec proposes a tiny shared `<ConnectionBadge connected={connected} />` presentational component
(text + colored dot, text is the accessible signal per every page spec's Accessibility criteria) rather than
duplicating the same three lines of JSX per page.

## Acceptance Criteria

1. **Given** `usePolling`'s `fetcher` resolves successfully on the first call, **when** the hook's state
   updates, **then** `loading` transitions `true → false` exactly once and never again for subsequent ticks
   (matching Target Behavior's "no full-page spinner on later ticks" rule).
2. **Given** a subsequent poll tick fails with `NetworkError`, **when** the hook updates, **then** `data`
   remains the last successfully fetched value (never cleared/nulled), `error` is set, and `connected`
   becomes `false` — every consuming page's degraded-mode Acceptance Criteria depend on this exact contract.
3. **Given** a subsequent poll tick fails with `ApiError` (e.g. a transient `500`), **when** the hook
   updates, **then** `connected` remains `true` (the server responded, it just errored) — distinguishing
   "server down" from "server rejected this specific call," per the deleted TUI's `ErrUnreachable`/`ErrAPI`
   split.
4. **Given** a component unmounts while a poll is in flight, **when** cleanup runs, **then** the in-flight
   `fetch` is aborted via `AbortController` and no state update occurs after unmount (no React
   "set state on unmounted component" warning) — verified via `frontend-01`'s `AbortError` passthrough
   contract.
5. **Given** `StrategyTable` is rendered with an empty `strategies` array, **when** it renders, **then** it
   shows "No strategies configured." rather than an empty table body — matching `tui-08` AC#5.
6. **Given** `StrategyTable`'s `variant="full"` and a `selectedId` matching one row, **when** it renders,
   **then** that row is visibly highlighted (a CSS class, not color-alone — also a distinct border/background
   pattern) — web equivalent of `tui-08` AC#4's terminal row-highlight requirement.
7. **Given** `ModeBadge`'s `pending` prop is `true`, **when** it renders, **then** the caption text is
   present in the accessible tree (not a `::after` CSS pseudo-element with no text content) — screen readers
   must be able to announce "pending, next cycle."
8. **Given** `ConfirmDialog` is opened, **when** `Escape` is pressed or the backdrop is clicked, **then**
   `onCancel` fires (never `onConfirm`) — an accidental dismiss must never be interpreted as approval.
9. **Given** `ConfirmDialog`'s `destructive` prop is set, **when** it renders, **then** the confirm button is
   visually distinct (red) **and** its accessible name still states the action in words (e.g. "Confirm close
   position"), not just "Confirm" relying on color to convey stakes.
10. **Edge case — `PositionCard`'s `priceStale` is `true`**: **when** it renders, **then** the last-known
    price is shown dimmed/with a small warning icon, and the accessible name for that price element includes
    "stale" or "last known" text — not conveyed by dimming alone.

## Out of Scope

- `AccountSummaryCard` — small enough to live directly in `frontend-03`'s Dashboard spec rather than as a
  cross-page shared component (it currently has exactly one consumer).
- A generic toast/notification system — referenced by several page specs (`frontend-04`, `frontend-05`) as
  "a toast (frontend-10's toast pattern)" but not fully specified here; implementers should treat this as a
  small addition to this spec's component set at implementation time (a `<Toast>` + a lightweight
  imperative `showToast()` API, mirroring the pattern the `frontend-specialist` skill's vanilla-JS reference
  already documents for this codebase's other frontend surface) rather than inventing a bespoke one per page.
- Any component-level caching/memoization beyond `usePolling`'s own "last successfully fetched value held in
  hook state" — matching the deleted TUI's `tui-08` Out of Scope precedent.

## Dependencies

- `frontend-01-scaffold-and-api-client.md` — `NetworkError`/`ApiError`, all `api/types.ts` interfaces
  consumed by these components' props.
- `docs/specs/phase-3/tui-08-shared-components.md`, `tui-04-page-positions.md` — feature/data-parity
  checklist (mode-badge colors, P&L tiering, empty-state copy).
- Consumed by: `frontend-03` (Dashboard), `frontend-04` (Strategies), `frontend-05` (Positions), and
  indirectly by every other page via `usePolling`.

## Judgment Calls (flagged for explicit reconciliation)

1. **`usePolling` is a single generic hook parameterized by a fetcher function, not a family of
   resource-specific hooks** (e.g. no dedicated `useAccount()`/`useStrategies()`). This keeps the primitive
   small and composable (matching ADR-008's "kept minimal" stance) at the cost of each page having to supply
   its own `fetcher` closure — a small amount of repeated boilerplate across pages, judged acceptable over
   introducing a heavier data-fetching abstraction this early.
