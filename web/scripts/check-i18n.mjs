#!/usr/bin/env node
// i18n guardrails (i18n-01 §5). Plain Node, no dependencies. Runs in
// `npm run build` before tsc.
//
//  1. Every locale has exactly the key set of `en` (missing -> fail, extra -> fail).
//  2. Every {placeholder} in an EN string is present in the other locales (-> fail).
//  3. Keys never referenced from src/ are reported (warning only).
//  4. Raw user-facing string literals in JSX - text nodes and
//     title= / placeholder= / aria-label= / alt= attributes - outside
//     src/i18n/ fail the check. Genuine non-text (symbols, "—", units) is
//     allowed; a line can opt out with an `i18n-ignore` comment.
//
// Usage: node scripts/check-i18n.mjs [--quiet]

import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const srcDir = path.join(root, 'src');
const localeDir = path.join(srcDir, 'i18n', 'locales');
const quiet = process.argv.includes('--quiet');

let errors = 0;
let warnings = 0;
const err = (msg) => {
  errors++;
  console.error(`  error: ${msg}`);
};
const warn = (msg) => {
  warnings++;
  if (!quiet) console.warn(`  warn:  ${msg}`);
};

// --- load locale catalogs -------------------------------------------------------
// Locale files are `const x[: Messages] = { ...plain object literal... };` - the
// literal is evaluated as JS (strings only, no code).
function loadCatalog(file) {
  const text = fs.readFileSync(path.join(localeDir, file), 'utf8');
  const start = text.indexOf('= {');
  const end = text.lastIndexOf('};');
  if (start < 0 || end < 0) throw new Error(`${file}: expected "const x = { ... };"`);
  const literal = text.slice(start + 2, end + 1);
  // eslint-disable-next-line no-new-func
  return new Function(`return (${literal});`)();
}

function flatten(obj, prefix = '', out = new Map()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === 'string') out.set(key, v);
    else if (v && typeof v === 'object') flatten(v, key, out);
    else throw new Error(`${key}: values must be strings or nested objects`);
  }
  return out;
}

const placeholders = (s) => new Set([...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]));
const PLURAL = new Set(['zero', 'one', 'other']);

const locales = { en: 'en.ts', es: 'es.ts', 'pt-BR': 'pt-BR.ts' };
const flat = {};
for (const [name, file] of Object.entries(locales)) flat[name] = flatten(loadCatalog(file));
const en = flat.en;

console.log(`check-i18n: ${en.size} keys in en`);

for (const [name, map] of Object.entries(flat)) {
  if (name === 'en') continue;
  for (const [key, value] of en) {
    if (!map.has(key)) {
      err(`${name}: missing key ${key}`);
      continue;
    }
    const want = placeholders(value);
    const got = placeholders(map.get(key));
    // A plural's one/zero form may spell the number out ("Fix the highlighted field").
    const pluralSingular = /\.(one|zero)$/.test(key);
    for (const p of want) {
      if (got.has(p) || (pluralSingular && p === 'count')) continue;
      err(`${name}: ${key} is missing placeholder {${p}}`);
    }
    for (const p of got) if (!want.has(p)) err(`${name}: ${key} has unknown placeholder {${p}}`);
  }
  for (const key of map.keys()) if (!en.has(key)) err(`${name}: extra key ${key} (not in en)`);
}

// --- source scan ----------------------------------------------------------------
function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(p, out);
    else if (/\.(tsx?|mjs)$/.test(entry.name)) out.push(p);
  }
  return out;
}

const files = walk(srcDir).filter((f) => !f.startsWith(path.join(srcDir, 'i18n') + path.sep));
const sources = files.map((f) => ({ file: f, text: fs.readFileSync(f, 'utf8') }));
const allText = sources.map((s) => s.text).join('\n');

// 3. unused keys (warning). A key counts as used when its full dotted path
// appears in a string literal, or when a prefix of it is used dynamically
// (`t.dyn(\`prefix.${x}\`)`, `t.enum('group', x)`).
const dynamicPrefixes = new Set();
for (const m of allText.matchAll(/`([a-zA-Z][\w.]*)\$\{/g)) if (m[1].includes(".")) dynamicPrefixes.add(m[1]);
for (const m of allText.matchAll(/\.enum\(\s*'(\w+)'/g)) dynamicPrefixes.add(`enums.${m[1]}`);
for (const m of allText.matchAll(/i18n-dynamic:\s*([\w.]+)/g)) dynamicPrefixes.add(m[1]);
const pluralBase = (key) => {
  const parts = key.split('.');
  return PLURAL.has(parts[parts.length - 1]) ? parts.slice(0, -1).join('.') : key;
};
const unused = [];
for (const key of en.keys()) {
  const k = pluralBase(key);
  if (allText.includes(`'${k}'`) || allText.includes(`"${k}"`) || allText.includes(`\`${k}\``)) continue;
  if ([...dynamicPrefixes].some((p) => k === p || k.startsWith(p.endsWith('.') ? p : `${p}.`) || (!p.endsWith('.') && k.startsWith(p)))) continue;
  unused.push(k);
}
for (const k of new Set(unused)) warn(`unused key ${k}`);

// 4. raw JSX strings.
// Allowed "text": no letters at all, or a short unit/symbol.
const ALLOWED_TEXT = /^(?:[^A-Za-zÀ-ÿ]*|[$%×x·•→←↑↓…—–]+|USD|USDT|ms|px|UTC|OK|ID|GTB|\d+[mhdwMs]|[A-Z]{2,6}(USDT?|BTC)(, ?[A-Z]+)*|Ctrl\+[^a-z]*|REPL|Lua|JSON|API|MCP|P&L|[OHLCV]:?|SL|TP|#\d*|v[\d.]+)$/;
function isAllowedText(s) {
  const t = s.replace(/&[a-z]+;|&#\d+;/g, '').trim();
  if (!t) return true;
  if (/^(https?:\/\/|\/)\S*$/.test(t)) return true; // URLs / paths (format hints)
  if (/^[*\d/ ,-]+$/.test(t)) return true; // cron expressions
  return ALLOWED_TEXT.test(t);
}

let rawCount = 0;
for (const { file, text } of sources) {
  if (!file.endsWith('.tsx')) continue;
  const rel = path.relative(root, file);
  const lines = text.split('\n');
  const lineOf = (idx) => text.slice(0, idx).split('\n').length;
  const ignored = (ln) => /i18n-ignore/.test(lines[ln - 1] ?? '') || /i18n-ignore/.test(lines[ln - 2] ?? '');

  // Text nodes: `>text<` where text has no JS-looking characters.
  for (const m of text.matchAll(/>([^<>{}]*?)</g)) {
    const raw = m[1];
    if (!/[A-Za-zÀ-ÿ]/.test(raw)) continue;
    if (/[=;()|&]|=>|\/\/|\*\//.test(raw)) continue; // code, not JSX text
    if (/\?\s*['"`]|['"`]\s*:/.test(raw)) continue; // ternary inside an expression
    if (/^\s*,/.test(raw) || (raw.includes('\n') && /^\s*\w+:\s*$/.test(raw))) continue; // object literal / generics
    if (/^\s*[A-Z0-9_.]+\s*$/.test(raw) && /[_.]/.test(raw)) continue; // config keys like AUTH.BOOTSTRAP_ADMIN_USERNAME
    // The `>` must close a JSX tag: `<Tag ...>` or `</Tag>` or `<>` - skip generics like Promise<void>.
    const before = text.slice(Math.max(0, m.index - 200), m.index + 1);
    if (!/(<[A-Za-z][\w.]*(\s[^<>]*)?>|<\/[A-Za-z][\w.]*>|<>|\/>|\}>)$/s.test(before)) continue;
    if (isAllowedText(raw)) continue;
    // A single identifier-like token in a code span (<code>, font-mono): a name, not prose.
    if (/^\s*[\w.:\-/]+\s*$/.test(raw) && /(<code[^>]*>|font-mono[^>]*>)$/s.test(before)) continue;
    if (/^\s*(Anthropic|Gemini|Discord|Slack|Telegram|Binance|Prometheus|Grafana|Asynqmon)\s*$/.test(raw)) continue;
    const ln = lineOf(m.index + 1);
    if (ignored(ln)) continue;
    rawCount++;
    err(`${rel}:${ln} JSX text "${raw.trim().slice(0, 60)}"`);
  }
  // Attributes.
  for (const m of text.matchAll(/\s(title|placeholder|aria-label|alt|label)=(?:"([^"]*)"|\{\s*'([^']*)'\s*\}|\{\s*"([^"]*)"\s*\})/g)) {
    const val = m[2] ?? m[3] ?? m[4] ?? '';
    if (!/[A-Za-zÀ-ÿ]/.test(val) || isAllowedText(val)) continue;
    const ln = lineOf(m.index);
    if (ignored(ln)) continue;
    rawCount++;
    err(`${rel}:${ln} ${m[1]}="${val.slice(0, 60)}"`);
  }
}

console.log(
  `check-i18n: ${errors} error(s), ${warnings} warning(s)` + (rawCount ? `, ${rawCount} raw JSX string(s)` : ''),
);
process.exit(errors ? 1 : 0);
