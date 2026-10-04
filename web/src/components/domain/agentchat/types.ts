export type { ChatDensity } from './cards/ChatCard';

// "Apply to editor" wiring, only passed by the dock while it is inside a
// Strategy Workbench (EditorBridgeContext registered). `strategyId` is the
// workbench's strategy; undefined for a not-yet-created draft, where any
// code card may be applied (same as the old floating widget).
export interface ApplyTarget {
  strategyId?: number;
  onApply: (source: string) => void;
}
