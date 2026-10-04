import React, { forwardRef, useEffect, useImperativeHandle, useRef } from 'react';
import { Loader2, Send } from 'lucide-react';
import { ChatDensity } from './types';
import { useT } from '@/i18n';

export interface ChatComposerHandle {
  focus: () => void;
}

// Multi-line message box (Phase D-02 §4): Enter sends, Shift+Enter inserts a
// newline, disabled while a turn is pending. Controlled so the Agent-mode
// suggested prompts can fill it.
export const ChatComposer = forwardRef<
  ChatComposerHandle,
  {
    value: string;
    onChange: (v: string) => void;
    onSubmit: () => void;
    disabled: boolean;
    density: ChatDensity;
    placeholder?: string;
  }
>(function ChatComposer({ value, onChange, onSubmit, disabled, density, placeholder }, ref) {
  const t = useT();
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  useImperativeHandle(ref, () => ({ focus: () => textareaRef.current?.focus() }), []);
  const compact = density === 'compact';
  const maxHeight = compact ? 160 : 240;

  // Grow with the content up to maxHeight, then scroll.
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`;
  }, [value, maxHeight]);

  const submit = () => {
    if (disabled || !value.trim()) return;
    onSubmit();
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      className="flex items-end gap-2"
    >
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            submit();
          }
        }}
        rows={compact ? 1 : 2}
        placeholder={placeholder ?? t('chat.composerPlaceholder')}
        disabled={disabled}
        aria-label={t('chat.composerAria')}
        data-testid="chat-composer-input"
        className={`form-input flex-1 resize-none leading-relaxed disabled:opacity-60 ${compact ? 'text-xs py-1.5' : 'text-sm py-2'}`}
        style={{ maxHeight }}
      />
      <button
        type="submit"
        disabled={disabled || !value.trim()}
        aria-label={t('chat.send')}
        data-testid="chat-send-btn"
        className={`bg-primary hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed text-primary-foreground font-semibold rounded flex items-center justify-center shrink-0 ${
          compact ? 'h-8 w-9' : 'h-10 w-11'
        }`}
      >
        {disabled ? <Loader2 className="w-4 h-4 animate-spin" /> : <Send className="w-4 h-4" />}
      </button>
    </form>
  );
});
