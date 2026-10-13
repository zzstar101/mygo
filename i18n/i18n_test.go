package i18n

import "testing"

func TestMatch(t *testing.T) {
	available := []string{"de", "en", "pt-BR", "sv", "zh-Hans", "zh-Hant"}
	for locale, want := range map[string]string{
		"de-DE":       "de",
		"de_AT.UTF-8": "de",
		"en-GB":       "en",
		"pt-BR":       "pt-BR",
		"pt-PT":       "pt-BR",
		"zh-Hans-CN":  "zh-Hans",
		"zh-CN":       "zh-Hans",
		"zh_SG":       "zh-Hans",
		"zh":          "zh-Hans",
		"zh-Hant-HK":  "zh-Hant",
		"zh-TW":       "zh-Hant",
		"zh_HK.UTF-8": "zh-Hant",
		"zh-MO":       "zh-Hant",
		"SV_se":       "sv",
		"es-419":      "",
		"sr-Latn-RS":  "",
		"":            "",
		"C":           "",
	} {
		if got := Match(locale, available); got != want {
			t.Errorf("Match(%q) = %q, want %q", locale, got, want)
		}
	}
	// One script of Chinese is no fallback for the other.
	if got := Match("zh-TW", []string{"en", "zh-Hans"}); got != "" {
		t.Errorf("Match(zh-TW) = %q, want no match", got)
	}
	// The regions of one script of Chinese are, and the two are compared by
	// language, script and region rather than by the spelling of the tags.
	if got := Match("zh-HK", []string{"en", "zh-TW"}); got != "zh-TW" {
		t.Errorf("Match(zh-HK, en and zh-TW) = %q, want zh-TW", got)
	}
	if got := Match("zh-TW", []string{"en", "zh-Hant-HK"}); got != "zh-Hant-HK" {
		t.Errorf("Match(zh-TW, en and zh-Hant-HK) = %q, want zh-Hant-HK", got)
	}
	if got := Match("zh-CN", []string{"en", "zh-SG"}); got != "zh-SG" {
		t.Errorf("Match(zh-CN, en and zh-SG) = %q, want zh-SG", got)
	}
	// The script of a tag counts for more than its region.
	if got := Match("sr-Latn-RS", []string{"sr-RS", "sr-Latn-BA"}); got != "sr-Latn-BA" {
		t.Errorf("Match(sr-Latn-RS, sr-RS and sr-Latn-BA) = %q, want sr-Latn-BA", got)
	}
	// A POSIX modifier names the script the locale is written in.
	if got := Match("sr_RS@latin", []string{"en", "sr-Latn-RS"}); got != "sr-Latn-RS" {
		t.Errorf("Match(sr_RS@latin, en and sr-Latn-RS) = %q, want sr-Latn-RS", got)
	}
	if got := Match("sr_RS@cyrillic", []string{"en", "sr-Cyrl-RS"}); got != "sr-Cyrl-RS" {
		t.Errorf("Match(sr_RS@cyrillic, en and sr-Cyrl-RS) = %q, want sr-Cyrl-RS", got)
	}
	// The script it names is the one that suits, and the other one is a
	// variant of the language like any other tag of a different script.
	if got := Match("sr_RS@latin", []string{"en", "sr-Cyrl-RS"}); got != "sr-Cyrl-RS" {
		t.Errorf("Match(sr_RS@latin, en and sr-Cyrl-RS) = %q, want sr-Cyrl-RS", got)
	}
	if got := Match("uz_UZ@cyrillic", []string{"uz-Latn-UZ"}); got != "uz-Latn-UZ" {
		t.Errorf("Match(uz_UZ@cyrillic, uz-Latn-UZ) = %q, want uz-Latn-UZ", got)
	}
	// A language and region the app has exactly.
	if got := Match("en-US", []string{"en-US", "en-GB"}); got != "en-US" {
		t.Errorf("Match(en-US) = %q, want en-US", got)
	}
	// Spanish of the two sides of the Atlantic are not variants of each
	// other: neither region matches, until the app has the Spanish the
	// system asked for.
	if got := Match("es-419", []string{"en", "es-ES"}); got != "" {
		t.Errorf("Match(es-419, en and es-ES) = %q, want no match", got)
	}
	if got := Match("es-MX", []string{"en", "es-ES"}); got != "" {
		t.Errorf("Match(es-MX, en and es-ES) = %q, want no match", got)
	}
	if got := Match("es-419", []string{"en", "es-419"}); got != "es-419" {
		t.Errorf("Match(es-419, en and es-419) = %q, want es-419", got)
	}
	if got := Match("es-419", []string{"en", "es"}); got != "es" {
		t.Errorf("Match(es-419, en and es) = %q, want es", got)
	}
	// A region the system did not name is the language itself, and the
	// Spanish the app has suits it.
	if got := Match("es", []string{"en", "es-ES"}); got != "es-ES" {
		t.Errorf("Match(es, en and es-ES) = %q, want es-ES", got)
	}
	// Portuguese variants still fall back to each other.
	if got := Match("pt-PT", []string{"en", "pt-BR"}); got != "pt-BR" {
		t.Errorf("Match(pt-PT, en and pt-BR) = %q, want pt-BR", got)
	}
}

func TestBaseLanguage(t *testing.T) {
	for tag, want := range map[string]string{
		"zh-Hant": "zh",
		"zh_Hant": "zh",
		"en":      "en",
		"EN-us":   "en",
		"":        "",
	} {
		if got := BaseLanguage(tag); got != want {
			t.Errorf("BaseLanguage(%q) = %q, want %q", tag, got, want)
		}
	}
}
