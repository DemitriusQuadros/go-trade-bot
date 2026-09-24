import { linter, Diagnostic } from '@codemirror/lint';
import { EditorView } from '@codemirror/view';

// Builds a single-element (or empty) Diagnostic[] from a fast-rerun/REPL
// response's error + error_line. Per ADR-027, this system's execution model
// is fail-fast-at-first-error (Runner.Eval/CallHook stop at the first error)
// - there is never more than one diagnostic to show at a time, so this
// function's return type is deliberately at-most-one-element, not a
// general-purpose multi-error mapper.
export function buildLuaDiagnostic(
  view: EditorView,
  error: string | undefined,
  errorLine: number | undefined
): Diagnostic[] {
  if (!error || !errorLine) return [];
  const lineCount = view.state.doc.lines;
  if (errorLine < 1 || errorLine > lineCount) return []; // defensive: a
    // stale/out-of-range line (e.g. the editor's text changed since the
    // error was captured) must not throw when doc.line() is called below
  const line = view.state.doc.line(errorLine);
  return [{ from: line.from, to: line.to, severity: 'error', message: error }];
}

// luaLintSource wraps a precomputed Diagnostic[] as a static linter()
// extension - diagnostics are computed OUTSIDE CodeMirror (from the
// fast-rerun response, not from parsing the document client-side; there is
// no client-side Lua parser in this stack and building one is out of scope,
// per blueprint §5), so this is intentionally not itself async/reactive -
// EditorPane recomputes the extensions array when diagnostics change, which
// is how @uiw/react-codemirror already propagates extension changes (no
// manual Compartment needed - this codebase's existing pattern of passing a
// fresh extensions array per render already handles this).
export function luaLintSource(diagnostics: Diagnostic[]) {
  return linter(() => diagnostics);
}
