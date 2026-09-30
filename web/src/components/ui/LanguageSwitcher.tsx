import React from 'react';
import { LOCALES, LOCALE_NATIVE, LOCALE_SHORT, useLocale, useT } from '@/i18n';

// EN / ES / PT segmented switcher (i18n-01 §2) - the login card and Profile.
export function LanguageSwitcher({ className = '', testId = 'language-switcher' }: { className?: string; testId?: string }) {
  const { locale, setLocale } = useLocale();
  const t = useT();
  return (
    <div
      role="radiogroup"
      aria-label={t('common.language')}
      data-testid={testId}
      className={`inline-flex items-center rounded-md border border-border bg-background p-0.5 ${className}`}
    >
      {LOCALES.map((l) => {
        const active = l === locale;
        return (
          <button
            key={l}
            type="button"
            role="radio"
            aria-checked={active}
            title={LOCALE_NATIVE[l]}
            lang={l}
            onClick={() => !active && setLocale(l)}
            data-testid={`${testId}-${l}`}
            className={`px-2 py-0.5 rounded text-[11px] font-semibold tracking-wide transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-ring ${
              active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-accent/60'
            }`}
          >
            {LOCALE_SHORT[l]}
          </button>
        );
      })}
    </div>
  );
}
