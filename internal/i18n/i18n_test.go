package i18n

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]Locale{"en": EN, "es": ES, "pt-BR": PTBR, "pt-br": PTBR, "PT_BR": PTBR, " es ": ES} {
		got, ok := Parse(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{"", "pt", "fr", "en-US", "xx"} {
		_, ok := Parse(in)
		assert.False(t, ok, in)
	}
	assert.Equal(t, ES, ParseOr("fr", ES))
}

func TestLanguageName(t *testing.T) {
	assert.Equal(t, "English", EN.LanguageName())
	assert.Equal(t, "Spanish (Latin America)", ES.LanguageName())
	assert.Equal(t, "Brazilian Portuguese", PTBR.LanguageName())
}

func TestFromAcceptLanguage(t *testing.T) {
	cases := map[string]Locale{
		"pt-BR,pt;q=0.9,en;q=0.8":      PTBR,
		"pt-PT":                        PTBR,
		"es-AR,es;q=0.9":               ES,
		"fr-FR,fr;q=0.9,es;q=0.5":      ES,
		"en-US,en;q=0.9":               EN,
		"de;q=0.9, pt;q=0.3, en;q=0.4": EN,
	}
	for h, want := range cases {
		got, ok := FromAcceptLanguage(h)
		assert.True(t, ok, h)
		assert.Equal(t, want, got, h)
	}
	for _, h := range []string{"", "*", "fr,de", "es;q=0"} {
		_, ok := FromAcceptLanguage(h)
		assert.False(t, ok, h)
	}
}

func TestCatalog(t *testing.T) {
	c := Catalog{EN: {"a": "A %s", "b": "B"}, ES: {"a": "A-es %s"}, PTBR: {"a": "A-pt %s", "b": "B-pt"}}
	assert.Equal(t, "A-es x", c.F(ES, "a", "x"))
	assert.Equal(t, "B", c.T(ES, "b"), "falls back to EN")
	assert.Equal(t, "zzz", c.T(PTBR, "zzz"), "falls back to the key")
	assert.Equal(t, map[Locale][]string{ES: {"b"}}, c.MissingKeys())
}

func TestCachedSource(t *testing.T) {
	calls := 0
	val, err := "pt-BR", error(nil)
	src := NewCachedSource(func(context.Context) (string, error) { calls++; return val, err }, time.Minute)
	now := time.Unix(1000, 0)
	src.now = func() time.Time { return now }

	assert.Equal(t, PTBR, src.DefaultLocale(context.Background()))
	val = "es"
	assert.Equal(t, PTBR, src.DefaultLocale(context.Background()), "cached")
	assert.Equal(t, 1, calls)

	now = now.Add(61 * time.Second)
	assert.Equal(t, ES, src.DefaultLocale(context.Background()))

	now = now.Add(61 * time.Second)
	err = errors.New("db down")
	assert.Equal(t, EN, src.DefaultLocale(context.Background()), "error -> en")

	now = now.Add(61 * time.Second)
	val, err = "", nil
	assert.Equal(t, EN, src.DefaultLocale(context.Background()), "empty -> en")

	var nilSrc *CachedSource
	assert.Equal(t, EN, nilSrc.DefaultLocale(context.Background()))
	assert.Equal(t, ES, Static("es").DefaultLocale(context.Background()))
}
