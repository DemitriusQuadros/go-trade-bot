import { EditorView } from '@codemirror/view';

// The Lua editor's own chrome (background/caret/selection/gutter) tuned
// per Console Pro theme - shared by every script-editing surface
// (EditorPane, ReplPane) so it cannot drift between them. Literal hex
// rather than hsl(var(--foo)) on purpose, mirroring how the chart
// components resolve theme colors: kept in sync BY HAND with index.css's
// --background/--card/--border/--foreground/--muted-foreground/--primary
// values for dark and light respectively - if those tokens change, update
// both hex pairs here too. CodeMirror's own built-in "dark"/"light" preset
// (passed as the <CodeMirror theme="..."> prop at each call site, not
// here) supplies the actual Lua syntax-highlighting palette; this theme
// only overrides the editor's surrounding chrome on top of that preset.
export const luaEditorDarkTheme = EditorView.theme(
  {
    '&': {
      backgroundColor: '#0B0D10 !important',
      color: '#E7ECEF',
    },
    '.cm-content': {
      caretColor: '#D9822B',
    },
    '&.cm-focused .cm-cursor': {
      borderLeftColor: '#D9822B',
    },
    '&.cm-focused .cm-selectionBackground, ::selection': {
      backgroundColor: '#232830 !important',
    },
    '.cm-gutters': {
      backgroundColor: '#14171B',
      color: '#8B95A1',
      border: 'none',
    },
  },
  { dark: true }
);

export const luaEditorLightTheme = EditorView.theme(
  {
    '&': {
      backgroundColor: '#FBF7EE !important',
      color: '#26211A',
    },
    '.cm-content': {
      caretColor: '#A8631D',
    },
    '&.cm-focused .cm-cursor': {
      borderLeftColor: '#A8631D',
    },
    '&.cm-focused .cm-selectionBackground, ::selection': {
      backgroundColor: '#E9DFC9 !important',
    },
    '.cm-gutters': {
      backgroundColor: '#F1E9D8',
      color: '#756B58',
      border: 'none',
    },
  },
  { dark: false }
);
