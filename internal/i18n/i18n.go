// Package i18n holds the supported UI/output locales (i18n-02 §1), a tiny
// message-catalog type shared by the server-rendered outputs (agent report
// HTML, backtest HTML report, webhook notifications), and a cached source
// for Settings.DefaultLocale.
//
// Only three locales exist: en (the default and the catalogs' source of
// truth), es (Latin-American Spanish) and pt-BR (Brazilian Portuguese).
// Their string values are an API contract shared with the frontend
// (User.Locale, Settings.DefaultLocale, ?lang=).
package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Locale is one supported locale ("en" | "es" | "pt-BR").
type Locale string

const (
	EN   Locale = "en"
	ES   Locale = "es"
	PTBR Locale = "pt-BR"
)

// Default is the locale used whenever nothing else is known.
const Default = EN

// Supported lists every supported locale, EN first.
var Supported = []Locale{EN, ES, PTBR}

// Parse returns the canonical Locale for s ("en", "es", "pt-BR";
// case-insensitive, "_" accepted for "-"). It reports false for "" and for
// anything outside the supported set.
func Parse(s string) (Locale, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
	for _, l := range Supported {
		if strings.EqualFold(s, string(l)) {
			return l, true
		}
	}
	return "", false
}

// ParseOr returns Parse(s), or fallback when s is not a supported locale.
func ParseOr(s string, fallback Locale) Locale {
	if l, ok := Parse(s); ok {
		return l
	}
	return fallback
}

// String implements fmt.Stringer.
func (l Locale) String() string { return string(l) }

// LanguageName is the English name of the locale's language, as used in the
// agent system prompt ("Always answer in <LanguageName> (<locale>).").
func (l Locale) LanguageName() string {
	switch l {
	case ES:
		return "Spanish (Latin America)"
	case PTBR:
		return "Brazilian Portuguese"
	}
	return "English"
}

// FromAcceptLanguage returns the best supported locale for an
// Accept-Language header: the highest-q language range whose primary tag
// maps to a supported locale ("pt*" -> pt-BR, "es*" -> es, "en*" -> en).
// Ties keep header order. It reports false when nothing matches.
func FromAcceptLanguage(header string) (Locale, bool) {
	type cand struct {
		loc   Locale
		q     float64
		order int
	}
	var cands []cand
	for i, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		tag := strings.ToLower(strings.TrimSpace(fields[0]))
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "q=") {
				if v, err := strconv.ParseFloat(strings.TrimPrefix(f, "q="), 64); err == nil {
					q = v
				}
			}
		}
		if q <= 0 {
			continue
		}
		primary := strings.SplitN(strings.ReplaceAll(tag, "_", "-"), "-", 2)[0]
		var loc Locale
		switch primary {
		case "pt":
			loc = PTBR
		case "es":
			loc = ES
		case "en":
			loc = EN
		default:
			continue
		}
		cands = append(cands, cand{loc: loc, q: q, order: i})
	}
	if len(cands) == 0 {
		return "", false
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].q > cands[b].q })
	return cands[0].loc, true
}

// Catalog is a per-locale message catalog. The EN map is the source of
// truth: T falls back to EN, then to the key itself.
type Catalog map[Locale]map[string]string

// T returns the message for key in l.
func (c Catalog) T(l Locale, key string) string {
	if m, ok := c[l]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := c[EN][key]; ok {
		return s
	}
	return key
}

// F formats the message for key in l with fmt verbs (use explicit argument
// indexes such as %[2]s when a translation reorders arguments).
func (c Catalog) F(l Locale, key string, args ...any) string {
	return fmt.Sprintf(c.T(l, key), args...)
}

// MissingKeys returns, per non-EN locale, the EN keys it lacks (sorted).
// An empty result means the catalog is complete.
func (c Catalog) MissingKeys() map[Locale][]string {
	out := map[Locale][]string{}
	for _, l := range Supported {
		if l == EN {
			continue
		}
		for k := range c[EN] {
			if _, ok := c[l][k]; !ok {
				out[l] = append(out[l], k)
			}
		}
		sort.Strings(out[l])
		if len(out[l]) == 0 {
			delete(out, l)
		}
	}
	return out
}
