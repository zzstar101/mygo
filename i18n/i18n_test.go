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
	// A language and region the app has exactly.
	if got := Match("en-US", []string{"en-US", "en-GB"}); got != "en-US" {
		t.Errorf("Match(en-US) = %q, want en-US", got)
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
