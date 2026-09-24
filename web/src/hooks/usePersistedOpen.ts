import { useEffect, useState } from 'react';

// Per-viewer open/closed UI preference, persisted across reloads via
// localStorage - guarded since a private window or cleared site data can
// make these throw or come back empty.
export function usePersistedOpen(key: string, defaultValue: boolean) {
  const [value, setValue] = useState(() => {
    try {
      const stored = window.localStorage.getItem(key);
      return stored === null ? defaultValue : stored === '1';
    } catch {
      return defaultValue;
    }
  });
  useEffect(() => {
    try {
      window.localStorage.setItem(key, value ? '1' : '0');
    } catch {
      // best-effort only
    }
  }, [key, value]);
  return [value, setValue] as const;
}

// Same idea as usePersistedOpen but for a small fixed set of string values
// (e.g. a 3-way view mode) instead of a boolean. `allowed` guards against a
// stale/foreign value left over in localStorage from a previous version.
export function usePersistedEnum<T extends string>(key: string, allowed: readonly T[], defaultValue: T) {
  const [value, setValue] = useState<T>(() => {
    try {
      const stored = window.localStorage.getItem(key) as T | null;
      return stored && (allowed as readonly string[]).includes(stored) ? stored : defaultValue;
    } catch {
      return defaultValue;
    }
  });
  useEffect(() => {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      // best-effort only
    }
  }, [key, value]);
  return [value, setValue] as const;
}

// Same idea again but for a single number (e.g. a draggable divider's
// position). No live-updating effect here on purpose - a drag can fire the
// setter dozens of times a second, and writing to localStorage on every one
// of those is wasted I/O; call `commit` (the second element) once at drag
// end instead of relying on this hook to persist every intermediate value.
export function usePersistedNumber(key: string, defaultValue: number) {
  const [value, setValue] = useState<number>(() => {
    try {
      const stored = window.localStorage.getItem(key);
      const parsed = stored === null ? NaN : Number(stored);
      return Number.isFinite(parsed) ? parsed : defaultValue;
    } catch {
      return defaultValue;
    }
  });
  const commit = (next: number) => {
    setValue(next);
    try {
      window.localStorage.setItem(key, String(next));
    } catch {
      // best-effort only
    }
  };
  return [value, setValue, commit] as const;
}
