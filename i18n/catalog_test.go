package i18n

import (
	"fmt"
	"slices"
	"strings"
	"sync"
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

// TestCatalogChange checks that Change keeps what a change leaves at its
// zero value, adds the language of a tag the catalog has none of from the
// texts it said for it, and that no reader sees a change half made.
func TestCatalogChange(t *testing.T) {
	c := New("en", map[string]greeting{
		"en":      {Hello: "Hello", Bye: "Bye"},
		"zh-Hant": {Hello: "你好", Bye: "再見"},
	})
	c.Change(map[string]greeting{
		"en":         {Bye: "Goodbye"},
		"zh-Hant-TW": {Hello: "您好"},
		"de":         {Hello: "Hallo"},
	})
	for locale, want := range map[string]greeting{
		"en-US": {Hello: "Hello", Bye: "Goodbye"},
		"zh-TW": {Hello: "您好", Bye: "再見"},
		"zh-HK": {Hello: "你好", Bye: "再見"},
		"de-AT": {Hello: "Hallo", Bye: "Goodbye"},
		"fr-FR": {Hello: "Hello", Bye: "Goodbye"},
	} {
		if got := *c.Get(locale); got != want {
			t.Errorf("%s says %+v, want %+v", locale, got, want)
		}
	}

	// Two goroutines that change different fields of one language keep
	// both changes.
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); c.Change(map[string]greeting{"fr": {Hello: fmt.Sprint("Bonjour ", i)}}) }()
		go func() { defer wg.Done(); c.Change(map[string]greeting{"fr": {Bye: fmt.Sprint("Au revoir ", i)}}) }()
	}
	wg.Wait()
	if got := *c.Get("fr"); !strings.HasPrefix(got.Hello, "Bonjour") || !strings.HasPrefix(got.Bye, "Au revoir") {
		t.Errorf("fr says %+v after concurrent changes", got)
	}
}

// TestCatalogGetAllocates holds Get, which the controls call as they
// build, to matching a locale without allocating.
func TestCatalogGetAllocates(t *testing.T) {
	c := New("en", map[string]greeting{"en": {Hello: "Hello"}, "zh-Hans": {Hello: "你好"}, "zh-Hant": {Hello: "你好"}})
	if n := testing.AllocsPerRun(100, func() { c.Get("zh-Hant-TW") }); n != 0 {
		t.Errorf("Get allocates %v times", n)
	}
}
