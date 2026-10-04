import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import en, { type Messages } from './locales/en';
import es from './locales/es';
import ptBR from './locales/pt-BR';

// In-house i18n (i18n-01 §1): a few hundred strings, three locales, no
// dependency. `en` is the source of truth; es / pt-BR are typed as exactly
// `typeof en`, so a missing key fails the build (and web/scripts/check-i18n.mjs
// also checks keys and {placeholders} before tsc runs).

export type Locale = 'en' | 'es' | 'pt-BR';
export const LOCALES: Locale[] = ['en', 'es', 'pt-BR'];
export const LOCALE_STORAGE_KEY = 'gtb_locale';

const CATALOGS: Record<Locale, Messages> = { en, es, 'pt-BR': ptBR };

// Short labels for the EN / ES / PT switcher, and the native names.
export const LOCALE_SHORT: Record<Locale, string> = { en: 'EN', es: 'ES', 'pt-BR': 'PT' };
export const LOCALE_NATIVE: Record<Locale, string> = { en: 'English', es: 'Español', 'pt-BR': 'Português (BR)' };

// --- key typing --------------------------------------------------------------

type PluralForms = { one: string; other: string; zero?: string };
type Join<K extends string, P extends string> = `${K}.${P}`;
// A key is a dotted path to a string leaf, or to a plural object ({one, other}).
type Paths<T> = {
  [K in keyof T & string]: T[K] extends string
    ? K
    : T[K] extends PluralForms
      ? K
      : Join<K, Paths<T[K]>>;
}[keyof T & string];

export type MessageKey = Paths<Messages>;
export type Vars = Record<string, string | number | null | undefined>;

// --- locale detection ---------------------------------------------------------

/** Maps any locale-ish string (from /auth/me, storage, navigator) to a supported Locale. */
export function normalizeLocale(raw: string | null | undefined): Locale | null {
  if (!raw) return null;
  const s = raw.trim().toLowerCase();
  if (!s) return null;
  if (s.startsWith('pt')) return 'pt-BR';
  if (s.startsWith('es')) return 'es';
  if (s.startsWith('en')) return 'en';
  return null;
}

function readStoredLocale(): Locale | null {
  try {
    return normalizeLocale(localStorage.getItem(LOCALE_STORAGE_KEY));
  } catch {
    return null;
  }
}

export function storeLocale(locale: Locale): void {
  try {
    localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    /* storage unavailable - the choice lasts for this page only */
  }
}

function browserLocale(): Locale {
  const langs = typeof navigator !== 'undefined' ? (navigator.languages?.length ? navigator.languages : [navigator.language]) : [];
  for (const l of langs) {
    const s = (l ?? '').toLowerCase();
    if (s.startsWith('pt')) return 'pt-BR';
    if (s.startsWith('es')) return 'es';
    if (s) return 'en';
  }
  return 'en';
}

/** Pre-login locale: localStorage `gtb_locale`, then navigator.languages (i18n-01 §2). */
export function initialLocale(): Locale {
  return readStoredLocale() ?? browserLocale();
}

// --- translation ---------------------------------------------------------------

// The active locale, readable from non-React code (api/client.ts error
// mapping, lib/format.ts). The provider keeps it in sync.
let activeLocale: Locale = initialLocale();

export function getLocale(): Locale {
  return activeLocale;
}

function lookup(catalog: unknown, key: string): unknown {
  let cur: unknown = catalog;
  for (const part of key.split('.')) {
    if (cur == null || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return cur;
}

const pluralRulesCache = new Map<Locale, Intl.PluralRules>();
function pluralRules(locale: Locale): Intl.PluralRules {
  let pr = pluralRulesCache.get(locale);
  if (!pr) {
    pr = new Intl.PluralRules(locale);
    pluralRulesCache.set(locale, pr);
  }
  return pr;
}

function interpolate(template: string, vars?: Vars): string {
  if (!vars) return template;
  return template.replace(/\{(\w+)\}/g, (m, name: string) => {
    const v = vars[name];
    return v == null ? m : String(v);
  });
}

function resolve(locale: Locale, key: string, vars?: Vars): string | undefined {
  const node = lookup(CATALOGS[locale], key);
  if (typeof node === 'string') return interpolate(node, vars);
  if (node && typeof node === 'object' && 'other' in node) {
    const forms = node as PluralForms;
    const count = Number(vars?.count ?? 0);
    let form: string | undefined;
    if (count === 0 && forms.zero != null) form = forms.zero;
    else form = pluralRules(locale).select(count) === 'one' ? forms.one : forms.other;
    return interpolate(form ?? forms.other, vars);
  }
  return undefined;
}

const warned = new Set<string>();
function warnMissing(locale: Locale, key: string) {
  if (!(import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV) return;
  const id = `${locale}:${key}`;
  if (warned.has(id)) return;
  warned.add(id);
  console.warn(`[i18n] missing key "${key}" for locale ${locale}`);
}

/** Translates `key` in `locale`: falls back to `en`, then to the key itself (dev warns). */
export function translate(locale: Locale, key: string, vars?: Vars): string {
  const hit = resolve(locale, key, vars);
  if (hit !== undefined) return hit;
  warnMissing(locale, key);
  const fallback = locale === 'en' ? undefined : resolve('en', key, vars);
  return fallback ?? key;
}

/** Whether `key` exists (in the active locale or en) - for dynamic enum lookups. */
export function hasKey(key: string): boolean {
  return resolve(activeLocale, key) !== undefined || resolve('en', key) !== undefined;
}

/** Translation in the active locale, for non-React code. */
export function tr(key: MessageKey, vars?: Vars): string {
  return translate(activeLocale, key, vars);
}

export interface TFunction {
  (key: MessageKey, vars?: Vars): string;
  /** A computed key (e.g. `enums.status.${s}`); returns `fallback` (or the key) when it's missing, without a warning. */
  dyn: (key: string, vars?: Vars, fallback?: string) => string;
  /** Like t(), but placeholders may be React nodes (links, <code>): returns a fragment. */
  rich: (key: MessageKey, vars: Record<string, React.ReactNode>) => React.ReactNode;
  /** Display label of an API enum value (`enums.<group>.<value>`); unknown values render as-is. */
  enum: (group: EnumGroup, value: string | null | undefined) => string;
  locale: Locale;
}

export type EnumGroup = keyof Messages['enums'];

function makeT(locale: Locale): TFunction {
  const t = ((key: MessageKey, vars?: Vars) => translate(locale, key, vars)) as TFunction;
  t.dyn = (key, vars, fallback) => {
    const hit = resolve(locale, key, vars) ?? resolve('en', key, vars);
    return hit ?? fallback ?? key;
  };
  t.rich = (key, vars) => {
    const raw = translate(locale, key);
    const parts = raw.split(/\{(\w+)\}/g);
    return React.createElement(
      React.Fragment,
      null,
      ...parts.map((part, i) =>
        i % 2 === 1
          ? React.createElement(React.Fragment, { key: i }, part in vars ? vars[part] : `{${part}}`)
          : part,
      ),
    );
  };
  t.enum = (group, value) => {
    if (value == null || value === '') return '';
    const hit = resolve(locale, `enums.${group}.${value}`) ?? resolve('en', `enums.${group}.${value}`);
    return hit ?? value;
  };
  t.locale = locale;
  return t;
}

// --- React binding -------------------------------------------------------------

// A language picked on the login card (no account to save it on yet). The
// auth bridge saves it to the account right after sign-in, so choosing PT-BR
// and then logging in keeps PT-BR (i18n-01 acceptance 2).
let pendingSignedOutChoice: Locale | null = null;
export function takePendingSignedOutChoice(): Locale | null {
  const l = pendingSignedOutChoice;
  pendingSignedOutChoice = null;
  return l;
}

type Persister = (locale: Locale) => void | Promise<void>;

interface I18nContextValue {
  locale: Locale;
  t: TFunction;
  /** Changes the UI language: immediate, stored in localStorage, <html lang>, and saved on the account when signed in. */
  setLocale: (locale: Locale) => void;
  /** Applies a locale without persisting it anywhere (e.g. the one /auth/me returned). */
  applyLocale: (locale: Locale) => void;
  /** Registered by the auth layer: saves the choice via PATCH /auth/me while signed in. */
  setPersister: (fn: Persister | null) => void;
}

const I18nContext = createContext<I18nContextValue | undefined>(undefined);

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(activeLocale);
  const persisterRef = React.useRef<Persister | null>(null);

  const apply = useCallback((l: Locale) => {
    activeLocale = l;
    setLocaleState(l);
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback(
    (l: Locale) => {
      apply(l);
      storeLocale(l);
      const persist = persisterRef.current;
      if (!persist) pendingSignedOutChoice = l;
      if (persist) {
        Promise.resolve(persist(l)).catch(() => {
          /* the UI already switched; the account keeps its old value until the next change */
        });
      }
    },
    [apply],
  );

  const setPersister = useCallback((fn: Persister | null) => {
    persisterRef.current = fn;
  }, []);

  const value = useMemo<I18nContextValue>(
    () => ({ locale, t: makeT(locale), setLocale, applyLocale: apply, setPersister }),
    [locale, setLocale, apply, setPersister],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

function useI18nContext(): I18nContextValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error('i18n hooks must be used inside I18nProvider');
  return ctx;
}

/** `t(key, vars?)` bound to the current locale. */
export function useT(): TFunction {
  return useI18nContext().t;
}

export function useLocale(): { locale: Locale; setLocale: (l: Locale) => void } {
  const { locale, setLocale } = useI18nContext();
  return { locale, setLocale };
}

/** Internal: used by the auth bridge (LocaleSync) only. */
export function useI18nInternals() {
  const { applyLocale, setPersister, locale } = useI18nContext();
  return { applyLocale, setPersister, locale };
}
