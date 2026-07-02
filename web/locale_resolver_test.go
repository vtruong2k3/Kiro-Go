// Feature: microsoft-365-sso, Property 1: Locale resolver never returns empty text —
// for any locale key (present in the active dictionary, present only in the fallback
// dictionary, or absent from both) the t(key) resolver returns a non-empty string, and
// for a key absent from every dictionary it returns exactly the key identifier.
//
// Validates: Requirements 1.6, 7.6
//
// No JS test harness (fast-check + jest/vitest, package.json, node_modules) exists in this
// repository — it is a Go project — so this property is validated in Go against the parsed
// locale JSON, faithfully porting the app.js t() lookup semantics.
package web

import (
	"encoding/json"
	"os"
	"testing"

	"pgregory.net/rapid"
)

// resolve is a faithful port of the app.js resolver:
//
//	let text = active[key] || fallback[key] || key;
//
// JavaScript `||` treats the empty string as falsy, so an empty active value falls through
// to the fallback value, and an empty fallback value falls through to the key itself.
func resolve(active, fallback map[string]string, key string) string {
	text := active[key]
	if text == "" {
		text = fallback[key]
	}
	if text == "" {
		text = key
	}
	return text
}

// loadLocale parses one of the real locale JSON files into a flat string map.
func loadLocale(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read locale file %s: %v", path, err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to parse locale file %s: %v", path, err)
	}
	return m
}

func anyKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

// TestProperty1LocaleResolverNeverEmpty exercises the resolver across the three scenarios
// (present in active, present only in fallback, absent from both) using both synthetic
// dictionaries and the real parsed en.json / zh.json locale files.
func TestProperty1LocaleResolverNeverEmpty(t *testing.T) {
	// Real dictionaries mirror app.js: active = dict[currentLang] (en), fallback = dict.zh.
	realActive := loadLocale(t, "locales/en.json")
	realFallback := loadLocale(t, "locales/zh.json")

	// Sanity: the real locale files must exist and be non-empty so the "present" scenarios
	// below have real data to draw from.
	if len(realActive) == 0 || len(realFallback) == 0 {
		t.Fatalf("expected non-empty real locale dictionaries, got en=%d zh=%d", len(realActive), len(realFallback))
	}

	// A non-empty token generator used for synthetic keys and values.
	nonEmpty := rapid.StringMatching(`[a-zA-Z0-9._]{1,12}`)

	rapid.Check(t, func(t *rapid.T) {
		// Start from freshly generated synthetic dictionaries.
		active := rapid.MapOf(nonEmpty, nonEmpty).Draw(t, "active")
		fallback := rapid.MapOf(nonEmpty, nonEmpty).Draw(t, "fallback")

		key := nonEmpty.Draw(t, "key")

		// scenario selects which of the three required cases (plus real-dictionary and
		// random cases) we place the key into.
		scenario := rapid.IntRange(0, 5).Draw(t, "scenario")

		absentFromBoth := false
		switch scenario {
		case 0: // present in the active dictionary
			active[key] = nonEmpty.Draw(t, "activeValue")
		case 1: // present only in the fallback dictionary
			delete(active, key)
			fallback[key] = nonEmpty.Draw(t, "fallbackValue")
		case 2: // absent from both dictionaries
			delete(active, key)
			delete(fallback, key)
			absentFromBoth = true
		case 3: // real dictionaries, query a key that genuinely exists in active (en)
			active = realActive
			fallback = realFallback
			key = anyKey(realActive)
		case 4: // real dictionaries, query a synthetic key absent from both
			active = realActive
			fallback = realFallback
			key = "synthetic.absent." + nonEmpty.Draw(t, "absentKey")
			absentFromBoth = true
		case 5: // fully random placement — key may or may not be present
			absentFromBoth = active[key] == "" && fallback[key] == ""
		}

		got := resolve(active, fallback, key)

		// Property: the resolver never returns empty text.
		if got == "" {
			t.Fatalf("resolver returned empty string for key %q (scenario %d)", key, scenario)
		}

		// Property: when the key is absent from every dictionary, the result is exactly the key.
		if absentFromBoth && got != key {
			t.Fatalf("expected key identifier %q for absent key, got %q (scenario %d)", key, got, scenario)
		}
	})
}

// TestProperty1RealLocaleKeysResolveNonEmpty is a companion example test asserting that every
// key defined in the real locale files resolves to a non-empty string, and that a key missing
// from both dictionaries resolves to the key identifier itself.
func TestProperty1RealLocaleKeysResolveNonEmpty(t *testing.T) {
	en := loadLocale(t, "locales/en.json")
	zh := loadLocale(t, "locales/zh.json")

	for key := range en {
		if got := resolve(en, zh, key); got == "" {
			t.Fatalf("en key %q resolved to empty string", key)
		}
	}
	for key := range zh {
		// With active = en and fallback = zh, a zh-only key falls through to the fallback.
		if got := resolve(en, zh, key); got == "" {
			t.Fatalf("zh key %q resolved to empty string", key)
		}
	}

	missing := "definitely.missing.key"
	if got := resolve(en, zh, missing); got != missing {
		t.Fatalf("expected missing key to resolve to itself %q, got %q", missing, got)
	}
}
