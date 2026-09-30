import React, { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { X, ChevronRight, ChevronLeft, Check } from 'lucide-react';
import { tr, type MessageKey } from '@/i18n';
import { useT } from '@/i18n';

export const WALKTHROUGH_STORAGE_KEY = 'gtb_walkthrough_completed';

export interface WalkthroughStep {
  targetSelector: string;
  title: string;
  body: string;
  route?: string;
}

// Title/body are read at access time from the i18n catalog (walkthrough.*).
const step = (targetSelector: string, key: string, route?: string): WalkthroughStep => ({
  targetSelector,
  route,
  get title() {
    return tr(`walkthrough.${key}Title` as MessageKey);
  },
  get body() {
    return tr(`walkthrough.${key}Body` as MessageKey);
  },
});

export const DEFAULT_WALKTHROUGH_STEPS: WalkthroughStep[] = [
  step('[data-walkthrough="nav-strategies"]', 'strategies'),
  step('[data-walkthrough="new-strategy-btn"]', 'build', '/strategies'),
  step('[data-walkthrough="mode-toggle"]', 'modes'),
  step('[data-walkthrough="nav-backtest"]', 'backtest'),
  step('[data-walkthrough="nav-settings"]', 'settings'),
  step('[data-walkthrough="nav-help"]', 'help'),
];

export interface WalkthroughProps {
  steps?: WalkthroughStep[];
  onComplete: () => void;
  onSkip: () => void;
}

export function Walkthrough({
  steps = DEFAULT_WALKTHROUGH_STEPS,
  onComplete,
  onSkip,
}: WalkthroughProps) {
  const t = useT();
  const [currentStepIdx, setCurrentStepIdx] = useState(0);
  const [targetRect, setTargetRect] = useState<DOMRect | null>(null);
  const navigate = useNavigate();
  const location = useLocation();
  const cardRef = useRef<HTMLDivElement>(null);

  const step = steps[currentStepIdx];
  const isLast = currentStepIdx === steps.length - 1;

  // Route sync
  useEffect(() => {
    if (step.route && location.pathname !== step.route) {
      navigate(step.route);
    }
  }, [step, location.pathname, navigate]);

  // Target element calculation
  useEffect(() => {
    let timer: NodeJS.Timeout;
    const updatePosition = () => {
      const el = document.querySelector(step.targetSelector);
      if (el) {
        setTargetRect(el.getBoundingClientRect());
      } else {
        setTargetRect(null);
      }
    };

    // Slight delay to allow DOM/route rendering to settle
    timer = setTimeout(updatePosition, 150);
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);

    return () => {
      clearTimeout(timer);
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [step, currentStepIdx, location.pathname]);

  // Keyboard navigation
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onSkip();
      }
    }
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onSkip]);

  const handleNext = () => {
    if (isLast) {
      onComplete();
    } else {
      setCurrentStepIdx((i) => i + 1);
    }
  };

  const handlePrev = () => {
    if (currentStepIdx > 0) {
      setCurrentStepIdx((i) => i - 1);
    }
  };

  // Calculate card position
  const getCardStyle = (): React.CSSProperties => {
    if (!targetRect) {
      return {
        position: 'fixed',
        top: '50%',
        left: '50%',
        transform: 'translate(-50%, -50%)',
      };
    }

    const margin = 16;
    let top = targetRect.bottom + margin;
    let left = targetRect.left;

    // Boundary checking
    if (top + 200 > window.innerHeight) {
      top = Math.max(margin, targetRect.top - 200 - margin);
    }
    if (left + 320 > window.innerWidth) {
      left = Math.max(margin, window.innerWidth - 320 - margin);
    }

    return {
      position: 'fixed',
      top: `${top}px`,
      left: `${left}px`,
    };
  };

  return (
    <div className="fixed inset-0 z-50 overflow-hidden" role="dialog" aria-modal="true">
      {/* Dimmed backdrop with spotlight box-shadow cutout if target element is found */}
      {targetRect ? (
        <div
          className="fixed rounded-md pointer-events-none transition-all duration-300 ease-out"
          style={{
            top: targetRect.top - 4,
            left: targetRect.left - 4,
            width: targetRect.width + 8,
            height: targetRect.height + 8,
            boxShadow: '0 0 0 9999px rgba(0, 0, 0, 0.75), 0 0 15px rgba(59, 130, 246, 0.5)',
            border: '2px solid rgba(59, 130, 246, 0.8)',
          }}
        />
      ) : (
        <div className="fixed inset-0 bg-background/75 transition-opacity" />
      )}

      {/* Screen-reader announcement */}
      <div className="sr-only" aria-live="polite">
        {t('walkthrough.stepAnnounce', { n: currentStepIdx + 1, total: steps.length, title: step.title, body: step.body })}
      </div>

      {/* Step Callout Card */}
      <div
        ref={cardRef}
        style={getCardStyle()}
        className="w-80 p-4 rounded-xl bg-card border border-border shadow-2xl text-foreground z-50 space-y-3 animate-in fade-in zoom-in-95 duration-200"
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded bg-secondary/60 text-foreground border border-border">
              {currentStepIdx + 1} / {steps.length}
            </span>
            <h3 className="text-sm font-semibold text-foreground">{step.title}</h3>
          </div>
          <button
            type="button"
            onClick={onSkip}
            className="text-muted-foreground hover:text-foreground p-1 rounded-md hover:bg-secondary"
            aria-label={t('walkthrough.skipAria')}
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="text-xs text-foreground leading-relaxed">{step.body}</p>

        <div className="flex items-center justify-between pt-2 border-t border-border">
          <button
            type="button"
            onClick={onSkip}
            className="text-xs text-muted-foreground hover:text-foreground px-2 py-1"
          >
            {t('walkthrough.skip')}
          </button>
          <div className="flex items-center gap-2">
            {currentStepIdx > 0 && (
              <button
                type="button"
                onClick={handlePrev}
                className="px-2.5 py-1.5 rounded text-xs text-foreground hover:bg-secondary flex items-center gap-1"
              >
                <ChevronLeft className="w-3.5 h-3.5" /> {t('common.back')}
              </button>
            )}
            <button
              type="button"
              onClick={handleNext}
              className="px-3 py-1.5 rounded text-xs font-semibold bg-primary hover:bg-primary text-white shadow flex items-center gap-1"
            >
              {isLast ? (
                <>
                  <Check className="w-3.5 h-3.5" /> {t('common.done')}
                </>
              ) : (
                <>
                  {t('common.next')} <ChevronRight className="w-3.5 h-3.5" />
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
