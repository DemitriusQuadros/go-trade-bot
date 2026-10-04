import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import {
  api,
  setAccessRequiredHandler,
  setUnauthorizedHandler,
} from '@/api/client';
import { Capability, LoginRequest, Me } from '@/api/types';
import { hasCapability } from '@/lib/permissions';

// Current user context (auth-02 §3). Everything capability-aware reads from
// here - no prop drilling. Mounted once in App.tsx, above AuthGate.

export type AuthStatus =
  | 'loading' // first GET /auth/me in flight
  | 'anonymous' // not signed in -> login screen
  | 'setup_required' // no users exist yet -> setup card
  | 'access_required' // Cloudflare Access session expired -> full-page message
  | 'error' // /auth/me failed for another reason (network) -> login screen with error
  | 'authenticated';

export interface AuthContextValue {
  status: AuthStatus;
  me: Me | null;
  /** Error from the initial /auth/me probe (e.g. the API is unreachable). */
  probeError: unknown;
  /** `admin` implies all capabilities. False while signed out. */
  can: (cap: Capability) => boolean;
  /** Re-reads /auth/me (e.g. after a chat turn to refresh today's spend). */
  refresh: () => Promise<void>;
  login: (req: LoginRequest) => Promise<void>;
  logout: () => Promise<void>;
  /** Set when a 401 bounced a signed-in user back to the login screen. */
  sessionExpired: boolean;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<AuthStatus>('loading');
  const [me, setMe] = useState<Me | null>(null);
  const [probeError, setProbeError] = useState<unknown>(null);
  const [sessionExpired, setSessionExpired] = useState(false);
  const statusRef = useRef(status);
  statusRef.current = status;

  const applyMe = useCallback(async (signal?: AbortSignal) => {
    const result = await api.getMe({ signal });
    if (result.kind === 'user') {
      setMe(result.me);
      setStatus('authenticated');
    } else {
      setMe(null);
      setStatus(result.kind === 'setup_required' ? 'setup_required' : 'anonymous');
    }
  }, []);

  // Initial probe.
  useEffect(() => {
    const controller = new AbortController();
    applyMe(controller.signal).catch((err) => {
      if (err instanceof DOMException && err.name === 'AbortError') return;
      if (statusRef.current === 'access_required') return;
      setProbeError(err);
      setStatus('error');
    });
    return () => controller.abort();
  }, [applyMe]);

  // Global handlers: any 401 -> login screen (the URL is kept, so the user
  // lands back on the same route after signing in); Cloudflare Access 403 ->
  // full-page message. Queries are cleared so no data survives the session.
  useEffect(() => {
    setUnauthorizedHandler(() => {
      if (statusRef.current === 'authenticated') setSessionExpired(true);
      setMe(null);
      setStatus('anonymous');
      queryClient.clear();
    });
    setAccessRequiredHandler(() => {
      setStatus('access_required');
      queryClient.clear();
    });
    return () => {
      setUnauthorizedHandler(null);
      setAccessRequiredHandler(null);
    };
  }, [queryClient]);

  // Back/forward cache: after signing out, Back can restore a frozen
  // pre-sign-out *document* (a different full page load) with its React state
  // and data intact - the server already refuses it (401), but the stale page
  // would still show the previous user's data until its next request.
  // Re-check the session whenever a page comes back from that cache.
  useEffect(() => {
    const onPageShow = (e: PageTransitionEvent) => {
      if (!e.persisted) return;
      queryClient.clear();
      applyMe().catch(() => {
        setMe(null);
        setStatus('anonymous');
      });
    };
    window.addEventListener('pageshow', onPageShow);
    return () => window.removeEventListener('pageshow', onPageShow);
  }, [applyMe, queryClient]);

  const refresh = useCallback(async () => {
    try {
      await applyMe();
    } catch {
      /* keep the current state - a transient failure shouldn't sign anyone out */
    }
  }, [applyMe]);

  const login = useCallback(
    async (req: LoginRequest) => {
      const res = await api.login(req);
      // Nothing from a previous session may leak into this one.
      queryClient.clear();
      setSessionExpired(false);
      setProbeError(null);
      // auth-01 contract (reconciled): login returns {user}; fall back to /auth/me if not.
      if (res?.user && typeof res.user.id === 'number') {
        setMe(res.user);
        setStatus('authenticated');
      } else {
        await applyMe();
      }
    },
    [applyMe, queryClient],
  );

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      /* the cookie may already be gone - sign out locally either way */
    }
    queryClient.clear();
    setMe(null);
    setSessionExpired(false);
    setStatus('anonymous');
    // The router is unmounted while the login screen shows; the next sign-in
    // starts from the dashboard rather than the previous user's page.
    try {
      window.history.replaceState(null, '', '/');
    } catch {
      /* ignore */
    }
  }, [queryClient]);

  const can = useCallback((cap: Capability) => hasCapability(me?.capabilities, cap), [me]);

  const value = useMemo<AuthContextValue>(
    () => ({ status, me, probeError, can, refresh, login, logout, sessionExpired }),
    [status, me, probeError, can, refresh, login, logout, sessionExpired],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside AuthProvider');
  return ctx;
}

/** Shorthand for components that only need `can`. */
export function useCan(): (cap: Capability) => boolean {
  return useAuth().can;
}
