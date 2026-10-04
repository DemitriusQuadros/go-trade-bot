import React, { useEffect, useRef, useState } from 'react';
import { Check, ChevronRight, MoreVertical } from 'lucide-react';
import { useT } from '@/i18n';

export interface DropdownMenuItem {
  label: string;
  icon?: React.ReactNode;
  // Either an action (onClick) or a link (href). A link item renders as an
  // <a> so middle-click / copy-link work; external links open in a new tab.
  onClick?: () => void;
  href?: string;
  external?: boolean;
  // Small icon after the label (e.g. an external-link glyph).
  trailingIcon?: React.ReactNode;
  destructive?: boolean;
  disabled?: boolean;
  // Visually separates this item from the ones above it (e.g. Delete at
  // the bottom of an otherwise-neutral action list).
  separatorBefore?: boolean;
  // Shows a check mark (e.g. the active language in a submenu).
  checked?: boolean;
  // A nested list, expanded inline under this item when clicked.
  submenu?: DropdownMenuItem[];
  // Stable React key when the label can change (translated labels).
  id?: string;
}

interface DropdownMenuProps {
  items: DropdownMenuItem[];
  align?: 'left' | 'right';
  label?: string;
  // Custom trigger contents (icon + text). Defaults to the kebab icon.
  trigger?: React.ReactNode;
  triggerClassName?: string;
}

// A single overflow menu for "one object, several actions" rows - a table
// row, a list card, anything that would otherwise need a button per
// action. Replaces stacking N buttons per row (which stops scaling the
// moment a fifth action shows up) with one trigger and a fixed list of
// items underneath it. Closes on outside click or Escape, same pattern
// HelpTooltip already established in this codebase.
const TRIGGER_DEFAULT_CLASS =
  'p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors';

export function DropdownMenu({
  items,
  align = 'right',
  label,
  trigger,
  triggerClassName = TRIGGER_DEFAULT_CLASS,
}: DropdownMenuProps) {
  const t = useT();
  const menuLabel = label ?? t('common.actions');
  const [open, setOpen] = useState(false);
  const [openSub, setOpenSub] = useState<string | null>(null);
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
        aria-label={menuLabel}
        aria-haspopup="menu"
        aria-expanded={open}
        title={trigger ? menuLabel : undefined}
        className={triggerClassName}
      >
        {trigger ?? <MoreVertical className="w-4 h-4" />}
      </button>

      {open && (
        <div
          role="menu"
          onClick={(e) => e.stopPropagation()}
          className={`absolute z-30 mt-1 min-w-[180px] py-1 rounded-md border border-border bg-popover shadow-lg ${
            align === 'right' ? 'right-0' : 'left-0'
          }`}
        >
          {items.map((item) => {
            const key = item.id ?? item.label;
            if (item.submenu) {
              const expanded = openSub === key;
              return (
                <React.Fragment key={key}>
                  {item.separatorBefore && <div className="my-1 border-t border-border" />}
                  <button
                    type="button"
                    role="menuitem"
                    aria-haspopup="menu"
                    aria-expanded={expanded}
                    onClick={() => setOpenSub(expanded ? null : key)}
                    className="w-full flex items-center gap-2.5 px-3 py-2 text-xs text-left transition-colors text-popover-foreground hover:bg-accent/60"
                  >
                    {item.icon && <span className="shrink-0 [&>svg]:w-3.5 [&>svg]:h-3.5">{item.icon}</span>}
                    <span className="whitespace-nowrap">{item.label}</span>
                    <ChevronRight className={`ml-auto w-3 h-3 opacity-60 transition-transform ${expanded ? 'rotate-90' : ''}`} />
                  </button>
                  {expanded && (
                    <div role="menu" aria-label={item.label} className="pb-1">
                      {item.submenu.map((sub) => (
                        <button
                          key={sub.id ?? sub.label}
                          type="button"
                          role="menuitemradio"
                          aria-checked={!!sub.checked}
                          disabled={sub.disabled}
                          onClick={() => {
                            setOpen(false);
                            setOpenSub(null);
                            sub.onClick?.();
                          }}
                          className="w-full flex items-center gap-2.5 pl-8 pr-3 py-1.5 text-xs text-left transition-colors text-popover-foreground hover:bg-accent/60 disabled:opacity-40"
                        >
                          <span className="whitespace-nowrap">{sub.label}</span>
                          {sub.checked && <Check className="ml-auto w-3 h-3 text-primary" />}
                        </button>
                      ))}
                    </div>
                  )}
                </React.Fragment>
              );
            }
            const itemClass = `w-full flex items-center gap-2.5 px-3 py-2 text-xs text-left transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
              item.destructive ? 'text-destructive hover:bg-destructive/10' : 'text-popover-foreground hover:bg-accent/60'
            }`;
            const content = (
              <>
                {item.icon && <span className="shrink-0 [&>svg]:w-3.5 [&>svg]:h-3.5">{item.icon}</span>}
                <span className="whitespace-nowrap">{item.label}</span>
                {item.checked && <Check className="ml-auto w-3 h-3 text-primary" />}
                {item.trailingIcon && (
                  <span className="ml-auto pl-2 shrink-0 opacity-60 [&>svg]:w-3 [&>svg]:h-3">{item.trailingIcon}</span>
                )}
              </>
            );
            return (
              <React.Fragment key={key}>
                {item.separatorBefore && <div className="my-1 border-t border-border" />}
                {item.href && !item.disabled ? (
                  <a
                    role="menuitem"
                    href={item.href}
                    target={item.external ? '_blank' : undefined}
                    rel={item.external ? 'noopener noreferrer' : undefined}
                    aria-label={item.external ? t('common.opensNewTab', { label: item.label }) : undefined}
                    onClick={() => {
                      setOpen(false);
                      item.onClick?.();
                    }}
                    className={itemClass}
                  >
                    {content}
                  </a>
                ) : (
                  <button
                    type="button"
                    role="menuitem"
                    disabled={item.disabled}
                    onClick={() => {
                      setOpen(false);
                      item.onClick?.();
                    }}
                    className={itemClass}
                  >
                    {content}
                  </button>
                )}
              </React.Fragment>
            );
          })}
        </div>
      )}
    </div>
  );
}
