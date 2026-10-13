package ui

import (
	"testing"
	"time"
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
