import React from 'react';
import { AlertTriangle, X } from 'lucide-react';

interface ConfirmDialogProps {
  isOpen: boolean;
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  isDangerous?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  isOpen,
  title,
  message,
  confirmText = 'Confirm',
  cancelText = 'Cancel',
  isDangerous = false,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  if (!isOpen) return null;

  return (
    // `modal-overlay`/`modal-content`/`btn*` were all dead classes (no CSS
    // rule ever defined them, same issue found and fixed across the
    // workbench panes) - this dialog rendered with no backdrop, no
    // positioning, and no button styling at all, effectively invisible/
    // easy-to-miss whenever triggered (used for delete confirmations in
    // Strategies/Settings/CandleImport). Real fixed-position overlay now.
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
      <div className="w-full max-w-md rounded-lg border border-border bg-card p-5 shadow-xl">
        <div className="flex items-start justify-between mb-4">
          <div className="flex items-center gap-3">
            {isDangerous && (
              <div className="p-2 bg-destructive/15 text-destructive rounded-lg">
                <AlertTriangle className="w-5 h-5" />
              </div>
            )}
            <h3 className="text-base font-semibold text-foreground">{title}</h3>
          </div>
          <button
            onClick={onCancel}
            className="text-muted-foreground hover:text-foreground p-1"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        <p className="text-sm text-muted-foreground mb-6">{message}</p>

        <div className="flex justify-end gap-3">
          <button
            onClick={onCancel}
            className="bg-secondary hover:bg-accent text-foreground rounded border border-border px-4 py-2 text-sm"
          >
            {cancelText}
          </button>
          <button
            onClick={onConfirm}
            className={
              isDangerous
                ? 'bg-destructive/20 hover:bg-destructive/30 text-destructive rounded border border-destructive/40 px-4 py-2 text-sm font-semibold'
                : 'bg-primary hover:bg-primary/90 text-primary-foreground rounded border border-primary px-4 py-2 text-sm font-semibold'
            }
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  );
}
