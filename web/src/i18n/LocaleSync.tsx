import { useEffect } from 'react';
import { api } from '@/api/client';
import { useAuth } from '@/context/AuthContext';
import { normalizeLocale, storeLocale, takePendingSignedOutChoice, useI18nInternals } from '@/i18n';

// Bridges the account and the UI language (i18n-01 §2). Priority: the saved
// `locale` from /auth/me, then localStorage `gtb_locale` (the pre-login
// choice), then navigator.languages. While signed in, a change is saved via
// PATCH /auth/me {locale}.
// i18n-02 contract (reconciled): assumes PATCH /auth/me accepts {locale: 'en'|'es'|'pt-BR'} and /auth/me echoes it back.
export function LocaleSync() {
  const { status, me, refresh } = useAuth();
  const { applyLocale, setPersister } = useI18nInternals();
  const signedIn = status === 'authenticated' && !!me;
  const saved = me?.locale;

  // Save changes on the account while signed in.
  useEffect(() => {
    if (!signedIn) {
      setPersister(null);
      return;
    }
    setPersister(async (l) => {
      await api.updateMe({ locale: l });
      await refresh();
    });
    return () => setPersister(null);
  }, [signedIn, setPersister, refresh]);

  // Apply the account's locale on sign-in / reload.
  useEffect(() => {
    if (!signedIn) return;
    const chosen = takePendingSignedOutChoice();
    if (chosen) {
      // Picked on the login card just now - that wins, and becomes the saved one.
      applyLocale(chosen);
      if (normalizeLocale(saved) !== chosen) {
        api.updateMe({ locale: chosen }).then(refresh, () => {
          /* the UI already uses it; the account keeps its old value */
        });
      }
      return;
    }
    const fromAccount = normalizeLocale(saved);
    if (fromAccount) {
      applyLocale(fromAccount);
      storeLocale(fromAccount);
    }
  }, [signedIn, saved, applyLocale, refresh]);

  return null;
}
