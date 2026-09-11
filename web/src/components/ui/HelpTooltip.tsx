import React, { useState, useId, useEffect, useRef } from 'react';
import { Info } from 'lucide-react';

export interface HelpTooltipProps {
  children: React.ReactNode;
  label?: string;
  side?: 'top' | 'bottom' | 'left' | 'right';
}

const positionClasses: Record<'top' | 'bottom' | 'left' | 'right', string> = {
  top: 'bottom-full left-1/2 -translate-x-1/2 mb-2',
  bottom: 'top-full left-1/2 -translate-x-1/2 mt-2',
  left: 'right-full top-1/2 -translate-y-1/2 mr-2',
  right: 'left-full top-1/2 -translate-y-1/2 ml-2',
};

export function HelpTooltip({ children, label = 'More information', side = 'top' }: HelpTooltipProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const containerRef = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    if (!open) return;

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setOpen(false);
      }
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
    <span ref={containerRef} className="relative inline-flex items-center">
      <button
        type="button"
        aria-describedby={open ? id : undefined}
        aria-label={label}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onClick={() => setOpen((o) => !o)}
        className="text-slate-500 hover:text-slate-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-full p-0.5 inline-flex items-center justify-center cursor-pointer transition-colors"
      >
        <Info className="w-3.5 h-3.5" />
      </button>
      {open && (
        <span
          role="tooltip"
          id={id}
          className={`absolute z-50 w-64 p-2.5 rounded-md bg-slate-900 border border-slate-700 text-xs text-slate-200 shadow-xl pointer-events-none leading-relaxed ${positionClasses[side]}`}
        >
          {children}
        </span>
      )}
    </span>
  );
}
