import { EditorView } from '@codemirror/view';

// The Hacker Lime theme's CodeMirror override, shared by every script-editing
// surface (ScriptEditor, ScriptRepl) so it cannot drift between them.
export const luaEditorDarkTheme = EditorView.theme(
  {
    '&': {
      backgroundColor: '#050505 !important',
      color: '#e2e8f0',
    },
    '.cm-content': {
      caretColor: '#10b981',
    },
    '&.cm-focused .cm-cursor': {
      borderLeftColor: '#10b981',
    },
    '&.cm-focused .cm-selectionBackground, ::selection': {
      backgroundColor: '#166534 !important',
    },
    '.cm-gutters': {
      backgroundColor: '#000000',
      color: '#475569',
      border: 'none',
    },
  },
  { dark: true }
);
