package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/i18n"
)

// texts are the strings of the controls package ui draws, in one language:
// what they say when the app does not say it for them.
type texts struct {
	// Weekdays are the initials of the days of the week, Sunday first as
	// time.Weekday counts them.
	Weekdays [7]string
	// FirstWeekday is the day a calendar's week starts on.
	FirstWeekday time.Weekday
	// PreviousMonth and NextMonth label the calendar's month buttons.
	PreviousMonth, NextMonth string
	// MonthYear writes a month and its year, as a calendar's title:
	// "January 2026".
	MonthYear func(year int, month time.Month) string
	// Day writes a day, as assistive technology reads it: "January 2,
	// 2026". It is also the order of the parts of a date.
	Day func(year int, month time.Month, day int) string
}

// textLanguages are the languages the controls of package ui say their own
// texts in. App.SetLocale of another language leaves them in English.
var textLanguages = []string{"en", "zh-Hans", "zh-Hant"}

// textsByLanguage are the strings of the controls, by language.
var textsByLanguage = map[string]*texts{
	"en": {
		Weekdays:      [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"},
		FirstWeekday:  time.Monday,
		PreviousMonth: "Previous month",
		NextMonth:     "Next month",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%s %d", month, year) },
		Day:           func(year int, month time.Month, day int) string { return fmt.Sprintf("%s %d, %d", month, day, year) },
	},
	"zh-Hans": {
		Weekdays:      [7]string{"日", "一", "二", "三", "四", "五", "六"},
		FirstWeekday:  time.Monday,
		PreviousMonth: "上个月",
		NextMonth:     "下个月",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%d年%d月", year, int(month)) },
		Day: func(year int, month time.Month, day int) string {
			return fmt.Sprintf("%d年%d月%d日", year, int(month), day)
		},
	},
	"zh-Hant": {
		Weekdays:      [7]string{"日", "一", "二", "三", "四", "五", "六"},
		FirstWeekday:  time.Monday,
		PreviousMonth: "上個月",
		NextMonth:     "下個月",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%d年%d月", year, int(month)) },
		Day: func(year int, month time.Month, day int) string {
			return fmt.Sprintf("%d年%d月%d日", year, int(month), day)
		},
	},
}

// textsFor returns the strings of the controls for locale: those of its
// language, or English when the framework has none of them.
func textsFor(locale string) *texts {
	if t, ok := textsByLanguage[i18n.Match(locale, textLanguages)]; ok {
		return t
	}
	return textsByLanguage["en"]
}
