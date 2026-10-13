package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
)

func TestTextsFor(t *testing.T) {
	if got := textsFor("zh-Hant-TW").NextMonth; got != "下個月" {
		t.Errorf("zh-Hant-TW says %q", got)
	}
	if got := textsFor("zh-Hant-TW").Weekdays[time.Monday]; got != "一" {
		t.Errorf("zh-Hant-TW calls Monday %q", got)
	}
	if got := textsFor("zh-Hans").MonthYear(2026, time.October); got != "2026年10月" {
		t.Errorf("zh-Hans writes October 2026 as %q", got)
	}
	// A language the framework has no texts of, and none at all, are
	// English.
	for _, locale := range []string{"fr-FR", "ja-JP", ""} {
		if got := textsFor(locale); got != textsFor("en") {
			t.Errorf("%q says %q", locale, got.PreviousMonth)
		}
	}
}

// TestTextsComplete holds every language the controls ship with to saying
// all of their own texts: a language added with a field left out fails here
// rather than showing nothing.
func TestTextsComplete(t *testing.T) {
	if got := controlTexts.Fallback(); got != "en" {
		t.Errorf("the controls fall back to %q", got)
	}
	for tag, tx := range builtinTexts {
		v := reflect.ValueOf(tx)
		for i := 0; i < v.NumField(); i++ {
			name, f := v.Type().Field(i).Name, v.Field(i)
			switch f.Kind() {
			case reflect.String:
				if f.String() == "" {
					t.Errorf("%s says no %s", tag, name)
				}
			case reflect.Func:
				if f.IsNil() {
					t.Errorf("%s says no %s", tag, name)
				}
			case reflect.Array:
				for j := 0; j < f.Len(); j++ {
					if f.Index(j).String() == "" {
						t.Errorf("%s says no %s[%d]", tag, name, j)
					}
				}
			case reflect.Ptr:
				if f.IsNil() {
					t.Errorf("%s says no %s", tag, name)
				} else if d := time.Weekday(f.Elem().Int()); d < time.Sunday || d > time.Saturday {
					t.Errorf("%s says %s is %v", tag, name, d)
				}
			}
		}
	}
	if got, want := controlTexts.Languages(), []string{"en", "zh-Hans", "zh-Hant"}; !slices.Equal(got, want) {
		t.Errorf("the controls say %q", got)
	}
}

// TestUseTextsKeepsZero checks that a field UseTexts leaves at its zero
// value keeps what the language says, even where the zero value is a day of
// its own, as Sunday is.
func TestUseTextsKeepsZero(t *testing.T) {
	defer resetControlTexts()
	base := *textsFor("en")
	UseTexts(map[string]Texts{"en": {NextMonth: "Weiter"}})
	got := textsFor("en")
	if got.NextMonth != "Weiter" || got.PreviousMonth != base.PreviousMonth || got.firstWeekday() != base.firstWeekday() {
		t.Errorf("UseTexts kept %q and %v, changed %q", got.PreviousMonth, got.firstWeekday(), got.NextMonth)
	}
	UseTexts(map[string]Texts{"en": {Weekdays: [7]string{"a", "b", "c", "d", "e", "f", "g"}, FirstWeekday: Weekday(time.Sunday)}})
	if got := textsFor("en"); got.Weekdays[0] != "a" || got.firstWeekday() != time.Sunday || got.NextMonth != "Weiter" {
		t.Errorf("UseTexts kept %q, changed %v and %q", got.NextMonth, got.firstWeekday(), got.Weekdays[0])
	}
	UseTexts(map[string]Texts{"en": {More: ""}})
	if got := textsFor("en"); got.firstWeekday() != time.Sunday {
		t.Errorf("UseTexts kept %v", got.firstWeekday())
	}
}

// TestUseTextsAtomic changes the texts of one language from several
// goroutines at once, each a field of its own, which all stay.
func TestUseTextsAtomic(t *testing.T) {
	defer resetControlTexts()
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(2)
		go func() { defer wg.Done(); UseTexts(map[string]Texts{"de": {Find: fmt.Sprint("Suchen ", i)}}) }()
		go func() { defer wg.Done(); UseTexts(map[string]Texts{"de": {More: fmt.Sprint("Mehr ", i)}}) }()
	}
	wg.Wait()
	if got := textsFor("de"); !strings.HasPrefix(got.Find, "Suchen") || !strings.HasPrefix(got.More, "Mehr") {
		t.Errorf("de says %q and %q", got.Find, got.More)
	}
}

// TestEngineKeepsTexts checks that a frame finds the texts of the controls
// without matching the locale again, and that a new locale or UseTexts
// gives it new ones.
func TestEngineKeepsTexts(t *testing.T) {
	defer resetControlTexts()
	var got []*Texts
	tt := coreNewTester(func(c *context) {
		got = append(got, c.rt.ownTexts(), c.rt.public.Texts())
	}, 100, 100)
	first := got[0]
	tt.Frame()
	for _, tx := range got {
		if tx != first {
			t.Fatal("the frames matched the locale again")
		}
	}
	if n := testing.AllocsPerRun(50, func() { tt.rt.ownTexts() }); n != 0 {
		t.Errorf("ownTexts allocates %v times", n)
	}
	tt.SetLocale("zh-Hans")
	if tx := got[len(got)-1]; tx.Find != "查找" {
		t.Errorf("after SetLocale the controls say %q", tx.Find)
	}
	UseTexts(map[string]Texts{"zh-Hans": {Find: "寻找"}})
	tt.Frame()
	if tx := got[len(got)-1]; tx.Find != "寻找" {
		t.Errorf("after UseTexts the controls say %q", tx.Find)
	}
}

// TestUseTexts changes the texts of a language, adds the texts of a
// language the framework has none of, and shows both in a window.
func TestUseTexts(t *testing.T) {
	defer resetControlTexts()
	tokens := []string{"Ada"}
	tt := coreNewTester(func(c *context) {
		coreColumn(c).Padding(10).Children(func() {
			coreTokenField(c, &tokens, nil)
		})
	}, 400, 120)
	if !tt.HasText("Remove Ada") {
		t.Fatalf("texts %q", tt.Texts())
	}
	UseTexts(map[string]Texts{
		"en": {
			Find:        "Suchen",
			RemoveToken: func(token string) string { return "Drop " + token },
		},
		"de": {NextMonth: "Weiter"},
	})
	tt.Frame()
	if !tt.HasText("Drop Ada") || tt.HasText("Remove Ada") {
		t.Errorf("after UseTexts the texts are %q", tt.Texts())
	}
	// What a change left alone keeps what the language said.
	en := textsFor("en")
	if en.Find != "Suchen" || en.PreviousMonth != "Previous month" || en.More != "More" {
		t.Errorf("en says %q, %q and %q", en.Find, en.PreviousMonth, en.More)
	}
	// A language added takes what it says over English, which stays for
	// what it does not say.
	de := textsFor("de-DE")
	if de.NextMonth != "Weiter" || de.PreviousMonth != "Previous month" || de.Day(2026, time.October, 2) != "October 2, 2026" {
		t.Errorf("de says %q, %q and %q", de.NextMonth, de.PreviousMonth, de.Day(2026, time.October, 2))
	}
	if !slices.Contains(controlTexts.Languages(), "de") {
		t.Errorf("the controls say %q", controlTexts.Languages())
	}
}

// resetControlTexts puts back the texts the controls ship with, for a test
// that changed them with UseTexts.
func resetControlTexts() {
	controlTexts = i18n.New[Texts]("en", builtinTexts)
	textsVersion.Add(1)
}
