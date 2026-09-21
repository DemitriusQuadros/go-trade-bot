import { createContext, useContext, useEffect, useMemo, useState, createElement, ReactNode } from 'react';

// Bridges the floating AI copilot widget (mounted once at the app-shell
// level in App.tsx, as a SIBLING of <Routes> - see AgentCopilotWidget.tsx)
// across into the strategy workbench's nested route tree, which owns the
// actual CodeMirror instance (EditorPane.tsx via WorkbenchShell.tsx).
//
// Because the widget lives outside the routed subtree that owns the editor,
// a plain Context.Provider rendered BY WorkbenchShell would only be visible
// to WorkbenchShell's own descendants, never to a sibling like the widget -
// context only flows down the render tree, not sideways. So the Provider
// itself lives at the App root (EditorBridgeProvider, wrapping both <Routes>
// and the widget), holding the bridge as state; WorkbenchShell REGISTERS
// into that shared state via useRegisterEditorBridge on mount/update and
// unregisters on unmount (leaving the workbench route), and the widget just
// reads the current value via useEditorBridge - undefined whenever no
// workbench route is currently mounted.
export interface EditorBridge {
  /** The editor's current (live, unsaved) Lua source. */
  currentSource: string;
  /** Pushes a new source string into the live CodeMirror instance. Only
   * ever called after the widget's own diff/preview confirmation - never
   * silently, since the operator may have in-progress edits. */
  applyScript: (source: string) => void;
  /** The strategy currently open in the workbench, if this is an edit
   * (not a brand-new, not-yet-saved strategy). */
  strategyId?: number;
}

interface EditorBridgeContextValue {
  bridge: EditorBridge | undefined;
  setBridge: (bridge: EditorBridge | undefined) => void;
}

const EditorBridgeContext = createContext<EditorBridgeContextValue | undefined>(undefined);

export function EditorBridgeProvider({ children }: { children: ReactNode }) {
  const [bridge, setBridge] = useState<EditorBridge | undefined>(undefined);
  const value = useMemo(() => ({ bridge, setBridge }), [bridge]);
  return createElement(EditorBridgeContext.Provider, { value }, children);
}

/** Consumed by AgentCopilotWidget: the currently-registered bridge, or
 * undefined when no strategy workbench route is mounted. */
export function useEditorBridge(): EditorBridge | undefined {
  const ctx = useContext(EditorBridgeContext);
  return ctx?.bridge;
}

/** Consumed by WorkbenchShell: registers `bridge` as the live editor bridge
 * for as long as this component stays mounted, clearing it on unmount so a
 * stale bridge (pointing at an editor that no longer exists) never lingers
 * after navigating away from the workbench. */
export function useRegisterEditorBridge(bridge: EditorBridge): void {
  const ctx = useContext(EditorBridgeContext);
  useEffect(() => {
    ctx?.setBridge(bridge);
    return () => ctx?.setBridge(undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ctx, bridge]);
}
