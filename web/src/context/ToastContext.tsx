import React, { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { ToastContainer, ToastMessage, ToastType } from '@/components/ui/Toast';

// App-wide toast queue backing the (previously unused) ui/Toast container -
// one provider in App.tsx so any page or the sidebar kill switch can confirm
// an action ("Run queued") without each rendering its own container.
interface ToastApi {
  toast: (message: string, type?: ToastType) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback((message: string, type: ToastType = 'success') => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    setToasts((prev) => [...prev, { id, type, message }]);
  }, []);

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
