package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/i18n"
)

// Weekday returns a day a calendar's week starts on, for
// Texts.FirstWeekday: a nil FirstWeekday keeps the day the language starts
// its week on.
func Weekday(d time.Weekday) *time.Weekday { return &d }

// Texts are the strings the controls of package ui say of their own, in one
// language: what they say when the app does not say it for them.
//
// The controls say them in the language of the locale the app shows
// (mygo.App.SetLocale), and UseTexts changes or adds them. A translation
// leaves what it does not change at its zero value, which keeps what the
// language it is for says, or what the fallback language says.
type Texts struct {
	// Weekdays are the initials of the days of the week, Sunday first as
	// time.Weekday counts them.
	Weekdays [7]string
	// FirstWeekday is the day a calendar's week starts on, as Weekday
	// gives it, or nil to keep the language's.
	FirstWeekday *time.Weekday
	// PreviousMonth and NextMonth label the calendar's month buttons.
	PreviousMonth, NextMonth string
	// MonthYear writes a month and its year, as a calendar's title:
	// "January 2026".
	MonthYear func(year int, month time.Month) string
	// Day writes a day, as assistive technology reads it: "January 2,
	// 2026". It is also the order of the parts of a date.
	Day func(year int, month time.Month, day int) string

	// Undo, Cut, Copy, Paste, Delete and SelectAll are the items of the
	// menu a text field shows.
	Undo, Cut, Copy, Paste, Delete, SelectAll string

	// Find labels the bar that searches text, and names what its field
	// shows while it is empty.
	Find string
	// Search names what an empty search field shows.
	Search string
	// Clear labels the button that clears a search field.
	Clear string
	// RemoveToken labels the button that removes a token of a token field,
	// as in "Remove Ada".
	RemoveToken func(token string) string

	// Saturation labels the area of a color picker that sets the
	// saturation and the brightness.
	Saturation string
	// Hex labels the field of a color picker that holds the color as
	// hexadecimal.
	Hex string

	// Divider labels the line between the panes of a split view.
	Divider string

	// DeveloperTools and Elements label the inspector and the list of the
	// elements of the window.
	DeveloperTools, Elements string

	// More labels the button of a toolbar that opens what the toolbar
	// hides.
	More string
}

// builtinTexts are the texts the controls ship with, in the languages the
// framework has them in.
var builtinTexts = map[string]Texts{
	"en": {
		Weekdays:      [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"},
		FirstWeekday:  Weekday(time.Monday),
		PreviousMonth: "Previous month",
		NextMonth:     "Next month",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%s %d", month, year) },
		Day:           func(year int, month time.Month, day int) string { return fmt.Sprintf("%s %d, %d", month, day, year) },

		Undo: "Undo", Cut: "Cut", Copy: "Copy", Paste: "Paste", Delete: "Delete", SelectAll: "Select All",

		Find:        "Find",
		Search:      "Search",
		Clear:       "Clear",
		RemoveToken: func(token string) string { return "Remove " + token },

		Saturation: "Saturation and brightness",
		Hex:        "Hex",

		Divider: "Divider",

		DeveloperTools: "Developer tools",
		Elements:       "Elements",

		More: "More",
	},
	"zh-Hans": {
		Weekdays:      [7]string{"日", "一", "二", "三", "四", "五", "六"},
		FirstWeekday:  Weekday(time.Monday),
		PreviousMonth: "上个月",
		NextMonth:     "下个月",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%d年%d月", year, int(month)) },
		Day: func(year int, month time.Month, day int) string {
			return fmt.Sprintf("%d年%d月%d日", year, int(month), day)
		},

		Undo: "撤销", Cut: "剪切", Copy: "复制", Paste: "粘贴", Delete: "删除", SelectAll: "全选",

		Find:        "查找",
		Search:      "搜索",
		Clear:       "清除",
		RemoveToken: func(token string) string { return "移除" + token },

		Saturation: "饱和度与亮度",
		Hex:        "十六进制",

		Divider: "分隔条",

		DeveloperTools: "开发者工具",
		Elements:       "元素",

		More: "更多",
	},
	"zh-Hant": {
		Weekdays:      [7]string{"日", "一", "二", "三", "四", "五", "六"},
		FirstWeekday:  Weekday(time.Monday),
		PreviousMonth: "上個月",
		NextMonth:     "下個月",
		MonthYear:     func(year int, month time.Month) string { return fmt.Sprintf("%d年%d月", year, int(month)) },
		Day: func(year int, month time.Month, day int) string {
			return fmt.Sprintf("%d年%d月%d日", year, int(month), day)
		},

		Undo: "復原", Cut: "剪下", Copy: "複製", Paste: "貼上", Delete: "刪除", SelectAll: "全選",

		Find:        "尋找",
		Search:      "搜尋",
		Clear:       "清除",
		RemoveToken: func(token string) string { return "移除" + token },

		Saturation: "飽和度與亮度",
		Hex:        "十六進位",

		Divider: "分隔線",

		DeveloperTools: "開發者工具",
		Elements:       "元素",

		More: "更多",
	},
}

// controlTexts are the texts of the controls, in every language the
// framework has them in, as the app changed them. A locale of another
// language says English.
var controlTexts = i18n.New[Texts]("en", builtinTexts)

// UseTexts changes the texts the controls say of their own, or adds the
// texts of languages the framework has none of. A language's texts are its
// tag's, as "zh-Hant-TW": its own language's with the changes the app made.
//
//	ui.UseTexts(map[string]ui.Texts{
//		"zh-Hans": {NextMonth: "下一月"},
//		"de":      {Find: "Suchen", Search: "Suchen", Clear: "Löschen"},
//	})
//
// It is safe from any goroutine, and the windows shown draw their controls
// again with the texts it set.
func UseTexts(texts map[string]Texts) {
	for tag, t := range texts {
		controlTexts.Use(tag, mergeTexts(*textsFor(tag), t))
	}
	textsChanged()
}

// mergeTexts returns the texts of a language with what over changes, which
// keeps what it leaves at a zero value.
func mergeTexts(t, over Texts) Texts {
	if over.Weekdays != ([7]string{}) {
		t.Weekdays = over.Weekdays
	}
	if over.FirstWeekday != nil {
		t.FirstWeekday = over.FirstWeekday
	}
	if over.PreviousMonth != "" {
		t.PreviousMonth = over.PreviousMonth
	}
	if over.NextMonth != "" {
		t.NextMonth = over.NextMonth
	}
	if over.MonthYear != nil {
		t.MonthYear = over.MonthYear
	}
	if over.Day != nil {
		t.Day = over.Day
	}
	if over.Undo != "" {
		t.Undo = over.Undo
	}
	if over.Cut != "" {
		t.Cut = over.Cut
	}
	if over.Copy != "" {
		t.Copy = over.Copy
	}
	if over.Paste != "" {
		t.Paste = over.Paste
	}
	if over.Delete != "" {
		t.Delete = over.Delete
	}
	if over.SelectAll != "" {
		t.SelectAll = over.SelectAll
	}
	if over.Find != "" {
		t.Find = over.Find
	}
	if over.Search != "" {
		t.Search = over.Search
	}
	if over.Clear != "" {
		t.Clear = over.Clear
	}
	if over.RemoveToken != nil {
		t.RemoveToken = over.RemoveToken
	}
	if over.Saturation != "" {
		t.Saturation = over.Saturation
	}
	if over.Hex != "" {
		t.Hex = over.Hex
	}
	if over.Divider != "" {
		t.Divider = over.Divider
	}
	if over.DeveloperTools != "" {
		t.DeveloperTools = over.DeveloperTools
	}
	if over.Elements != "" {
		t.Elements = over.Elements
	}
	if over.More != "" {
		t.More = over.More
	}
	return t
}

// firstWeekday returns the day a calendar's week starts on: the language's,
// or Monday when its texts say none.
func (t *Texts) firstWeekday() time.Weekday {
	if t.FirstWeekday == nil {
		return time.Monday
	}
	return *t.FirstWeekday
}

// textsFor returns the texts of the controls for locale: those of its
// language, as an app changed them, or English when the framework has none
// of them.
func textsFor(locale string) *Texts { return controlTexts.Get(locale) }
