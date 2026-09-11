import React, { useState, useEffect, type ReactNode } from 'react';
import {
  api,
  setToken,
  getToken,
  clearToken,
  setUnauthorizedHandler,
  ApiError,
  NetworkError,
} from '@/api/client';
import { KeyRound, ShieldAlert, Loader2, LogOut } from 'lucide-react';

interface AuthGateProps {
  children: ReactNode;
}

export function AuthGate({ children }: AuthGateProps) {
  const [unlocked, setUnlocked] = useState<boolean>(() => getToken() !== null);
  const [tokenInput, setTokenInput] = useState('');
  const [validating, setValidating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setUnauthorizedHandler(() => {
      clearToken();
      setUnlocked(false);
      setError('Session expired or token no longer valid — please re-enter it.');
    });
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const cleanToken = tokenInput.trim();
    if (!cleanToken) {
      setError('Token is required.');
      return;
    }
    setValidating(true);
    setError(null);
    setToken(cleanToken);

    try {
      await api.getAccount();
      setUnlocked(true);
    } catch (err) {
      clearToken();
      if (err instanceof ApiError && err.status === 401) {
        let message = 'Invalid token. Check your configuration and try again.';
        try {
          const parsed = JSON.parse(err.body);
          if (parsed.message) message = parsed.message;
        } catch {
          // ignore
        }
        setError(message);
      } else if (err instanceof NetworkError) {
        setError('Cannot reach server at :8080. Is cmd/api running?');
      } else {
        setError('Unexpected error validating token. Check browser console.');
      }
    } finally {
      setValidating(false);
    }
  }

  const handleLogout = () => {
    clearToken();
    setUnlocked(false);
    setTokenInput('');
    setError(null);
  };

  if (!unlocked) {
    return (
      <div className="min-h-screen flex items-center justify-center p-4 bg-slate-950 text-slate-100">
        <div className="card w-full max-w-md p-8 bg-slate-900 border-slate-800 shadow-2xl rounded-xl">
          <div className="flex justify-center mb-4">
            <div className="p-3 bg-blue-600/20 text-blue-400 rounded-full border border-blue-500/30">
              <KeyRound className="w-8 h-8" />
            </div>
          </div>
          <h1 className="text-xl font-bold text-center text-slate-100 mb-2">
            Authentication Required
          </h1>
          <p className="text-xs text-center text-slate-400 mb-6">
            Enter your configured GTB API token to access the trading dashboard.
          </p>

          {error && (
            <div className="mb-4 p-3 bg-red-950/50 border border-red-800/80 rounded-lg flex items-start gap-2 text-xs text-red-200">
              <ShieldAlert className="w-4 h-4 text-red-400 flex-shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label
                htmlFor="gtb-token"
                className="block text-xs font-semibold text-slate-300 uppercase tracking-wider mb-2"
              >
                API Token
              </label>
              <input
                id="gtb-token"
                type="password"
                value={tokenInput}
                onChange={(e) => setTokenInput(e.target.value)}
                placeholder="Enter API token..."
                disabled={validating}
                autoFocus
                className="w-full px-3 py-2 bg-slate-950 border border-slate-700 rounded-lg text-sm text-slate-100 placeholder-slate-500 focus:outline-none focus:border-blue-500 focus:ring-1 focus:ring-blue-500 transition-all font-mono"
              />
            </div>

            <button
              type="submit"
              disabled={validating}
              className="w-full btn btn-primary py-2.5 flex items-center justify-center gap-2 font-semibold text-sm"
            >
              {validating ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  <span>Validating...</span>
                </>
              ) : (
                <span>Unlock Dashboard</span>
              )}
            </button>
          </form>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex flex-col">
      {/* Top action helper (in case needed anywhere) */}
      <div className="hidden" id="auth-actions">
        <button onClick={handleLogout} title="Logout">
          <LogOut />
        </button>
      </div>
      {children}
    </div>
  );
}
