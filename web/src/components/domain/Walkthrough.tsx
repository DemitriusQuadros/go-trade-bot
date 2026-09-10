import React, { useState, useEffect, useRef } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { X, ChevronRight, ChevronLeft, Check } from 'lucide-react';

export const WALKTHROUGH_STORAGE_KEY = 'gtb_walkthrough_completed';

export interface WalkthroughStep {
  targetSelector: string;
  title: string;
  body: string;
  route?: string;
}

export const DEFAULT_WALKTHROUGH_STEPS: WalkthroughStep[] = [
  {
    targetSelector: '[data-walkthrough="nav-strategies"]',
    title: 'Strategies',
    body: 'Build, backtest, and run trading strategies from here — no code required.',
  },
  {
    targetSelector: '[data-walkthrough="new-strategy-btn"]',
    route: '/strategies',
    title: 'Build a strategy',
    body: 'This wizard walks you through scope, entry rules, sizing, and exits — no JSON editing needed.',
  },
  {
    targetSelector: '[data-walkthrough="nav-backtest"]',
    title: 'Backtest',
    body: 'Validate a strategy against historical data before risking real capital.',
  },
  {
    targetSelector: '[data-walkthrough="nav-settings"]',
    title: 'Settings',
    body: 'Broker credentials, trading mode, and monitoring links all live here.',
  },
  {
    targetSelector: '[data-walkthrough="nav-help"]',
    title: 'Help',
    body: 'Come back here any time you need a refresher — including replaying this walkthrough.',
  },
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
        <div className="fixed inset-0 bg-black/75 transition-opacity" />
      )}

      {/* Screen-reader announcement */}
      <div className="sr-only" aria-live="polite">
        {`Step ${currentStepIdx + 1} of ${steps.length}: ${step.title}. ${step.body}`}
      </div>

      {/* Step Callout Card */}
      <div
        ref={cardRef}
        style={getCardStyle()}
        className="w-80 p-4 rounded-xl bg-slate-900 border border-slate-700 shadow-2xl text-slate-100 z-50 space-y-3 animate-in fade-in zoom-in-95 duration-200"
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded bg-blue-900/60 text-blue-300 border border-blue-800">
              {currentStepIdx + 1} / {steps.length}
            </span>
            <h3 className="text-sm font-semibold text-white">{step.title}</h3>
          </div>
          <button
            type="button"
            onClick={onSkip}
            className="text-slate-400 hover:text-slate-200 p-1 rounded-md hover:bg-slate-800"
            aria-label="Skip walkthrough"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <p className="text-xs text-slate-300 leading-relaxed">{step.body}</p>

        <div className="flex items-center justify-between pt-2 border-t border-slate-800">
          <button
            type="button"
            onClick={onSkip}
            className="text-xs text-slate-400 hover:text-slate-200 px-2 py-1"
          >
            Skip
          </button>
          <div className="flex items-center gap-2">
            {currentStepIdx > 0 && (
              <button
                type="button"
                onClick={handlePrev}
                className="px-2.5 py-1.5 rounded text-xs text-slate-300 hover:bg-slate-800 flex items-center gap-1"
              >
                <ChevronLeft className="w-3.5 h-3.5" /> Back
              </button>
            )}
            <button
              type="button"
              onClick={handleNext}
              className="px-3 py-1.5 rounded text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white shadow flex items-center gap-1"
            >
              {isLast ? (
                <>
                  <Check className="w-3.5 h-3.5" /> Done
                </>
              ) : (
                <>
                  Next <ChevronRight className="w-3.5 h-3.5" />
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
