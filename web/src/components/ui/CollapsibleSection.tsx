import React, { ReactNode } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { usePersistedOpen } from '@/hooks/usePersistedOpen';
import { useT } from '@/i18n';

interface CollapsibleSectionProps {
  // Stable key for localStorage persistence - prefix with the page/pane
  // name (e.g. 'workbench.editor.config') so ids never collide across panes.
  id: string;
  title: string;
  subtitle?: string;
  defaultOpen?: boolean;
  // Extra controls shown in the header (e.g. Re-run/Reset buttons) -
  // clicks inside `action` don't toggle the section (stopPropagation).
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  // When true and open, the body fills the remaining height of a flex
  // parent (h-full flex-1 min-h-0, no padding) instead of a normal padded
  // block - for content that manages its own internal sizing/scrolling,
  // like a code editor that should stretch to fill available space.
  fill?: boolean;
  // When true, hides the section header bar and forces the section body to display
  // (e.g. for full-screen Zen Mode where parent chrome is replacing the header).
  hideHeader?: boolean;
}

// Per-component hide/show, distinct from WorkbenchShell's two top-bar
// panel toggles (which hide/show the ENTIRE side panel or console). This
// is the finer-grained "hide just this one card" control living next to
// each individual panel instead.
export function CollapsibleSection({
  id,
  title,
  subtitle,
  defaultOpen = true,
  action,
  children,
  className = '',
  fill = false,
  hideHeader = false,
}: CollapsibleSectionProps) {
  const t = useT();
  const [open, setOpen] = usePersistedOpen(`collapsible.${id}`, defaultOpen);
  const isActuallyOpen = hideHeader ? true : open;

  return (
    <div
      className={`rounded-lg border border-border bg-card overflow-hidden ${
        fill && isActuallyOpen ? 'h-full flex flex-col min-h-0' : ''
      } ${className}`}
    >
      {!hideHeader && (
        <div className="flex items-center justify-between gap-2 px-3 py-2 bg-secondary/60 border-b border-border text-xs shrink-0">
          <button
            onClick={() => setOpen((o) => !o)}
            className="flex items-center gap-1.5 min-w-0 text-left text-foreground hover:text-primary"
            title={open ? t('common.hideSection', { title: typeof title === 'string' ? title : '' }) : t('common.showSection', { title: typeof title === 'string' ? title : '' })}
          >
            {open ? <ChevronDown className="w-3.5 h-3.5 shrink-0" /> : <ChevronRight className="w-3.5 h-3.5 shrink-0" />}
            <span className="font-semibold text-[11px] uppercase tracking-wider truncate">{title}</span>
            {subtitle && <span className="text-[10px] text-muted-foreground truncate">{subtitle}</span>}
          </button>
          {action && (
            <div className="flex items-center gap-2 shrink-0" onClick={(e) => e.stopPropagation()}>
              {action}
            </div>
          )}
        </div>
      )}
      {isActuallyOpen && <div className={fill ? 'flex-1 min-h-0' : 'p-3'}>{children}</div>}
    </div>
  );
}
