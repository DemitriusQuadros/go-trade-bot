import React, { useEffect, useRef, useState } from 'react';
import { MoreVertical } from 'lucide-react';

export interface DropdownMenuItem {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  destructive?: boolean;
  disabled?: boolean;
  // Visually separates this item from the ones above it (e.g. Delete at
  // the bottom of an otherwise-neutral action list).
  separatorBefore?: boolean;
}

interface DropdownMenuProps {
  items: DropdownMenuItem[];
  align?: 'left' | 'right';
  label?: string;
}

// A single overflow menu for "one object, several actions" rows - a table
// row, a list card, anything that would otherwise need a button per
// action. Replaces stacking N buttons per row (which stops scaling the
// moment a fifth action shows up) with one trigger and a fixed list of
// items underneath it. Closes on outside click or Escape, same pattern
// HelpTooltip already established in this codebase.
export function DropdownMenu({ items, align = 'right', label = 'Actions' }: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false);
    }
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [open]);

  return (
    <div ref={containerRef} className="relative inline-block">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setOpen((o) => !o);
        }}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors"
      >
        <MoreVertical className="w-4 h-4" />
      </button>

      {open && (
        <div
          role="menu"
          onClick={(e) => e.stopPropagation()}
          className={`absolute z-30 mt-1 min-w-[180px] py-1 rounded-md border border-border bg-popover shadow-lg ${
            align === 'right' ? 'right-0' : 'left-0'
          }`}
        >
          {items.map((item, i) => (
            <React.Fragment key={item.label}>
              {item.separatorBefore && <div className="my-1 border-t border-border" />}
              <button
                type="button"
                role="menuitem"
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  item.onClick();
                }}
                className={`w-full flex items-center gap-2.5 px-3 py-2 text-xs text-left transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                  item.destructive
                    ? 'text-destructive hover:bg-destructive/10'
                    : 'text-popover-foreground hover:bg-accent/60'
                }`}
              >
                {item.icon && <span className="shrink-0 [&>svg]:w-3.5 [&>svg]:h-3.5">{item.icon}</span>}
                <span>{item.label}</span>
              </button>
            </React.Fragment>
          ))}
        </div>
      )}
    </div>
  );
}
