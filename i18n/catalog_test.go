package i18n

import (
	"slices"
	"testing"
)

type greeting struct {
	Hello string
	Bye   string
}

func TestCatalog(t *testing.T) {
	c := New("en", map[string]greeting{
		"en":      {Hello: "Hello", Bye: "Bye"},
		"zh-Hans": {Hello: "你好", Bye: "再见"},
	})
	if got, want := c.Fallback(), "en"; got != want {
		t.Errorf("the fallback language is %q", got)
	}
	if got, want := c.Languages(), []string{"en", "zh-Hans"}; !slices.Equal(got, want) {
		t.Errorf("the languages are %q", got)
	}
	if got := c.Get("zh-CN").Hello; got != "你好" {
		t.Errorf("zh-CN says %q", got)
	}
	if got := c.Get("zh-TW").Hello; got != "Hello" {
		t.Errorf("zh-TW says %q", got)
	}
	if got := c.Get("fr-FR").Bye; got != "Bye" {
		t.Errorf("fr-FR says %q", got)
	}
	if got := c.Match("en-GB"); got != "en" {
		t.Errorf("en-GB is %q", got)
	}
	if got := c.Match("fr-FR"); got != "" {
		t.Errorf("fr-FR is %q", got)
	}
	// Use replaces the texts of a language whole.
	c.Use("de", greeting{Hello: "Hallo"})
	if got := c.Get("de-DE").Hello; got != "Hallo" {
		t.Errorf("de-DE says %q", got)
	}
	if got := c.Get("de-DE").Bye; got != "" {
		t.Errorf("de-DE says %q of a text it never gave", got)
	}
	c.UseAll(map[string]greeting{"fr": {Hello: "Bonjour"}})
	if got := c.Get("fr").Hello; got != "Bonjour" {
		t.Errorf("fr says %q", got)
	}
	if got, want := c.Languages(), []string{"de", "en", "fr", "zh-Hans"}; !slices.Equal(got, want) {
		t.Errorf("the languages are %q", got)
	}
}

// TestCatalogFallbackMissing holds a fallback no language gave to returning
// zero values rather than nothing, so a caller that never checks can read
// the texts it got.
func TestCatalogFallbackMissing(t *testing.T) {
	c := New("en", map[string]greeting{"zh-Hans": {Hello: "你好"}})
	if got := c.Get("zh-Hans").Hello; got != "你好" {
		t.Errorf("zh-Hans says %q", got)
	}
	if got := c.Get("en-US"); *got != (greeting{}) {
		t.Errorf("en-US says %q", got.Hello)
	}
	if !slices.Contains(c.Languages(), "en") {
		t.Errorf("the languages are %q", c.Languages())
	}
}

func TestCatalogZeroValue(t *testing.T) {
	var c Catalog[greeting]
	if got := c.Get("en-US"); *got != (greeting{}) {
		t.Errorf("an empty catalog says %q", got.Hello)
	}
	if got := c.Languages(); len(got) != 0 {
		t.Errorf("an empty catalog says %q", got)
	}
	if got := c.Match("en"); got != "" {
		t.Errorf("an empty catalog matches %q", got)
	}
	if got := c.Fallback(); got != "" {
		t.Errorf("an empty catalog falls back to %q", got)
	}
	c.Use("en", greeting{Hello: "Hello"})
	if got := c.Get("en-US").Hello; got != "Hello" {
		t.Errorf("after Use, en-US says %q", got)
	}
}
