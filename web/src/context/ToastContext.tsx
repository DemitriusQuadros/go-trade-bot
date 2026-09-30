import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { setForbiddenHandler } from '@/api/client';
import { ToastContainer, ToastMessage, ToastType } from '@/components/ui/Toast';

// App-wide toast queue backing the (previously unused) ui/Toast container -
// one provider in App.tsx so any page or the sidebar kill switch can confirm
// an action ("Run queued") without each rendering its own container.
interface ToastApi {
  toast: (message: string, type?: ToastType) => void;
}

const DEDUPE_MS = 2000;

const ToastContext = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  // Identical toasts within DEDUPE_MS collapse into one: a 403 is toasted
  // globally (below) and a page's own onError may toast the same message.
  const recent = useRef(new Map<string, number>());
  const toast = useCallback((message: string, type: ToastType = 'success') => {
    const key = `${type}:${message}`;
    const now = Date.now();
    const last = recent.current.get(key);
    if (last != null && now - last < DEDUPE_MS) return;
    recent.current.set(key, now);
    const id = `${now}-${Math.random().toString(36).slice(2)}`;
    setToasts((prev) => [...prev, { id, type, message }]);
  }, []);

  // Any 403 on a mutation shows the backend's message (auth-02 §4) - the
  // backend stays the source of truth even where the UI hid a control.
  useEffect(() => {
    setForbiddenHandler((message) => toast(message, 'error'));
    return () => setForbiddenHandler(null);
  }, [toast]);

  const value = useMemo(() => ({ toast }), [toast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      {/* aria-live so screen readers announce action results. */}
      <div aria-live="polite">
        <ToastContainer toasts={toasts} onDismiss={dismiss} />
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    // Outside the provider (shouldn't happen) - degrade to a no-op rather
    // than crashing the page.
    return { toast: () => {} };
  }
  return ctx;
}
