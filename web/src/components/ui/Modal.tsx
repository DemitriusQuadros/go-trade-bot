import React, { useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { useT } from '@/i18n';

// Generic dialog shell - same overlay/card look as ConfirmDialog. Escape and
// the close button call onClose; focus moves into the dialog on open and
// back to the previously focused element on close; Tab stays inside.
export function Modal({
  title,
  onClose,
  children,
  footer,
  testId,
}: {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  testId?: string;
}) {
  const t = useT();
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const first = ref.current?.querySelector<HTMLElement>('input, select, textarea, button');
    first?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key !== 'Tab' || !ref.current) return;
      const focusable = Array.from(
        ref.current.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled])'),
      );
      if (focusable.length === 0) return;
      const firstEl = focusable[0];
      const lastEl = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === firstEl) {
        e.preventDefault();
        lastEl.focus();
      } else if (!e.shiftKey && document.activeElement === lastEl) {
        e.preventDefault();
        firstEl.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      previous?.focus?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        data-testid={testId}
        className="w-full max-w-lg max-h-[90vh] overflow-y-auto rounded-lg border border-border bg-card p-5 shadow-xl"
      >
        <div className="flex items-start justify-between mb-4">
          <h3 className="text-base font-semibold text-foreground">{title}</h3>
          <button type="button" onClick={onClose} aria-label={t('common.close')} className="text-muted-foreground hover:text-foreground p-1">
            <X className="w-5 h-5" />
          </button>
        </div>
        {children}
        {footer && <div className="mt-5 flex justify-end gap-3">{footer}</div>}
      </div>
    </div>
  );
}

export const MODAL_BUTTON_SECONDARY =
  'bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm disabled:opacity-50';
export const MODAL_BUTTON_PRIMARY =
  'bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold disabled:opacity-50 flex items-center gap-1.5';
