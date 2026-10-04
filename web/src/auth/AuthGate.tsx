import React, { useState, type ReactNode } from 'react';
import { ShieldAlert, Loader2, Settings2, CloudOff } from 'lucide-react';
import { ApiError, NetworkError, apiErrorMessage } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { LoadingScreen } from '@/components/ui/Spinner';
import { LanguageSwitcher } from '@/components/ui/LanguageSwitcher';
import { tr, useT } from '@/i18n';


// The Console Pro auth card shell shared by every pre-app screen.
function AuthCard({ children }: { children: ReactNode }) {
  const t = useT();
  return (
    <div className="min-h-screen flex items-center justify-center p-4 bg-background text-foreground">
      <div className="w-full max-w-md p-8 bg-card border border-border shadow-2xl rounded-xl">
        <div className="flex flex-col items-center mb-6">
          <img src="/gopher-face.png" alt="" className="w-14 h-14 rounded-xl object-cover mb-3" />
          <span className="font-semibold text-sm tracking-tight text-muted-foreground">{t('common.appName')}</span>
          <LanguageSwitcher className="mt-3" testId="login-language" />
        </div>
        {children}
      </div>
    </div>
  );
}

function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div
      role="alert"
      data-testid="login-error"
      className="mb-4 p-3 bg-destructive/15 border border-destructive/40 rounded-lg flex items-start gap-2 text-xs text-destructive"
    >
      <ShieldAlert className="w-4 h-4 text-destructive flex-shrink-0 mt-0.5" />
      <span>{children}</span>
    </div>
  );
}

function loginErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    // auth-01 contract (reconciled): 401 invalid_credentials, 429 rate_limited (§4).
    if (err.status === 401) return tr('errors.invalid_credentials');
    if (err.status === 429) return tr('errors.rate_limited');
    return apiErrorMessage(err, tr('auth.unexpected'));
  }
  if (err instanceof NetworkError) return tr('auth.unreachable');
  return tr('auth.unexpected');
}

function LoginScreen() {
  const { login, sessionExpired, probeError } = useAuth();
  const t = useT();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const notice =
    error ??
    (probeError ? loginErrorMessage(probeError) : sessionExpired ? t('auth.sessionExpired') : null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!username.trim() || !password) {
      setError(t('auth.required'));
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await login({ username: username.trim(), password });
    } catch (err) {
      setError(loginErrorMessage(err));
      setPassword('');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthCard>
      <h1 className="text-xl font-bold text-center text-foreground mb-1">{t('auth.signInTitle')}</h1>
      <p className="text-xs text-center text-muted-foreground mb-6">{t('auth.signInSubtitle')}</p>

      {notice && <ErrorBox>{notice}</ErrorBox>}

      <form onSubmit={handleSubmit} className="space-y-4" data-testid="login-form">
        <div>
          <label htmlFor="gtb-username" className="block text-xs font-semibold text-foreground uppercase tracking-wider mb-2">
            {t('auth.username')}
          </label>
          <input
            id="gtb-username"
            type="text"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            disabled={submitting}
            autoFocus
            data-testid="login-username"
            className="w-full px-3 py-2 bg-background border border-border rounded-lg text-sm text-foreground placeholder-muted-foreground focus:outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
          />
        </div>
        <div>
          <label htmlFor="gtb-password" className="block text-xs font-semibold text-foreground uppercase tracking-wider mb-2">
            {t('auth.password')}
          </label>
          <input
            id="gtb-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={submitting}
            data-testid="login-password"
            className="w-full px-3 py-2 bg-background border border-border rounded-lg text-sm text-foreground placeholder-muted-foreground focus:outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
          />
        </div>

        <button
          type="submit"
          disabled={submitting}
          data-testid="login-submit"
          className="w-full bg-primary hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed text-primary-foreground rounded-lg py-2.5 flex items-center justify-center gap-2 font-semibold text-sm transition-colors"
        >
          {submitting ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin" />
              <span>{t('auth.signingIn')}</span>
            </>
          ) : (
            <span>{t('auth.signIn')}</span>
          )}
        </button>
      </form>
    </AuthCard>
  );
}

function SetupRequiredScreen() {
  const t = useT();
  const [before, middle, after] = t('auth.setupBody', { usernameKey: '\u0000', passwordKey: '\u0000' }).split('\u0000');
  return (
    <AuthCard>
      <div className="flex items-center justify-center gap-2 mb-3 text-foreground">
        <Settings2 className="w-5 h-5 text-primary" />
        <h1 className="text-lg font-bold">{t('auth.setupTitle')}</h1>
      </div>
      <p className="text-sm text-muted-foreground text-center leading-relaxed" data-testid="setup-required">
        {before}
        <code className="font-mono text-xs text-foreground">AUTH.BOOTSTRAP_ADMIN_USERNAME</code>
        {middle}
        <code className="font-mono text-xs text-foreground">AUTH.BOOTSTRAP_ADMIN_PASSWORD</code>
        {after}
      </p>
    </AuthCard>
  );
}

function AccessRequiredScreen() {
  const t = useT();
  return (
    <AuthCard>
      <div className="flex items-center justify-center gap-2 mb-3 text-foreground">
        <CloudOff className="w-5 h-5 text-warning" />
        <h1 className="text-lg font-bold">{t('auth.accessTitle')}</h1>
      </div>
      <p className="text-sm text-muted-foreground text-center mb-5" data-testid="access-required">
        {t('errors.access_required')}
      </p>
      <button
        type="button"
        onClick={() => window.location.reload()}
        className="w-full bg-primary hover:bg-primary/90 text-primary-foreground rounded-lg py-2.5 font-semibold text-sm"
      >
        {t('auth.reload')}
      </button>
    </AuthCard>
  );
}

// Decides between the login screen and the app (auth-02 §2). The app tree
// (router, providers, chat state) is only mounted while signed in, so signing
// out tears all of it down together with the cleared query cache.
export function AuthGate({ children }: { children: ReactNode }) {
  const { status } = useAuth();
  const t = useT();

  switch (status) {
    case 'loading':
      return (
        <div className="min-h-screen flex items-center justify-center bg-background">
          <LoadingScreen message={t('auth.checkingSession')} />
        </div>
      );
    case 'setup_required':
      return <SetupRequiredScreen />;
    case 'access_required':
      return <AccessRequiredScreen />;
    case 'authenticated':
      return <div className="min-h-screen flex flex-col">{children}</div>;
    default:
      return <LoginScreen />;
  }
}
