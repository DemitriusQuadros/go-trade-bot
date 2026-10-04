import React from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { StreamLanguage } from '@codemirror/language';
import { lua } from '@codemirror/legacy-modes/mode/lua';
import { EditorView } from '@codemirror/view';
import { Diagnostic } from '@codemirror/lint';
import { luaEditorDarkTheme, luaEditorLightTheme } from '@/lib/codeMirrorTheme';
import { luaAutocompletion } from '@/lib/luaCompletions';
import { luaLintSource } from '@/lib/luaLinting';
import { useIsDarkMode } from '@/hooks/useIsDarkMode';

interface LuaScriptEditorProps {
  source: string;
  onChange: (value: string) => void;
  diagnostics: Diagnostic[];
  onCreateEditor: (view: EditorView) => void;
  // Fills the remaining height of a flex parent (CollapsibleSection's own
  // `fill` mode) instead of a fixed pixel height - see WorkbenchShell's
  // Script/Split/Chart view modes, where this editor should stretch to use
  // whatever vertical space is available rather than stopping at 500px.
  fill?: boolean;
  // auth-02 §4: a strategy this user can't change opens read-only.
  readOnly?: boolean;
}

// The Lua source editor, deliberately separate from StrategyMetadataForm so
// the two can be laid out, collapsed, and reasoned about independently.
export function LuaScriptEditor({ source, onChange, diagnostics, onCreateEditor, fill = false, readOnly = false }: LuaScriptEditorProps) {
  const isDark = useIsDarkMode();
  return (
    <div className={fill ? 'h-full text-sm bg-background/95' : 'text-sm bg-background/95 -m-3'}>
      <CodeMirror
        value={source}
        height={fill ? '100%' : '500px'}
        theme={isDark ? 'dark' : 'light'}
        onCreateEditor={onCreateEditor}
        readOnly={readOnly}
        editable={!readOnly}
        extensions={[
          StreamLanguage.define(lua),
          isDark ? luaEditorDarkTheme : luaEditorLightTheme,
          luaAutocompletion,
          luaLintSource(diagnostics),
        ]}
        onChange={onChange}
        className={fill ? 'font-mono text-xs h-full' : 'font-mono text-xs'}
        basicSetup={{
          lineNumbers: true,
          highlightActiveLineGutter: true,
          foldGutter: true,
        }}
      />
    </div>
  );
}
