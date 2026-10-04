// Locale-safe numeric text parsing for form inputs.
//
// <input type="number"> renders and parses using the browser's locale: under
// pt-BR (and most of Europe) a stored 1.1 displays as "1,1", and what the
// user types back is locale-interpreted too. These helpers back
// `type="text" inputMode="decimal"` inputs instead, so display and parsing
// are explicit and identical everywhere.
//
// Rules:
// - Exactly one decimal separator, "." or "," - both mean the decimal point.
//   "1.1" and "1,1" both parse to 1.1.
// - No thousands separators and no mixing: "1.000,5", "1,000.5" and "1.1.1"
//   are rejected (null), never guessed at - a guess is how a value ends up
//   10x or 1000x off.
// - Optional leading sign; a bare leading/trailing separator (".5", "5.")
//   is accepted. Whitespace around the value is ignored.
// - Anything else (empty, "abc", "1e3", "Infinity") returns null, never NaN.
const DECIMAL_RE = /^[+-]?(\d+([.,]\d*)?|[.,]\d+)$/;
const INTEGER_RE = /^[+-]?\d+$/;

export function parseDecimal(raw: string): number | null {
  const t = raw.trim();
  if (!DECIMAL_RE.test(t)) return null;
  const n = Number(t.replace(',', '.'));
  return Number.isFinite(n) ? n : null;
}

// Whole numbers only - no separator of either kind, so "1,000" or "1.0" is
// rejected rather than read as 1.
export function parseInteger(raw: string): number | null {
  const t = raw.trim();
  if (!INTEGER_RE.test(t)) return null;
  const n = Number(t);
  return Number.isSafeInteger(n) ? n : null;
}

// Display form for a number held in a text input: String() is locale-
// independent (always "." and no grouping), unlike toLocaleString().
export function formatDecimal(n: number | null | undefined): string {
  return n == null || !Number.isFinite(n) ? '' : String(n);
}
