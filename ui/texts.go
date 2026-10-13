package ui

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/i18n"
)

// Weekday returns a day a calendar's week starts on, for
// Texts.FirstWeekday: a nil FirstWeekday keeps the day the language starts
// its week on.
func Weekday(d time.Weekday) *time.Weekday { return &d }

// Texts are the strings the controls of package ui say of their own, in one
// language: what they say when the app does not say it for them. The strings
// of the code editor (package ui/editor) are among them, in the last group.
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

	// The strings of the code editor (package ui/editor).

	// CodeEditor names the editor to assistive technology when the app
	// gives it no label.
	CodeEditor string

	// GoToDefinition and ToggleComment label the editor's items that go to
	// the definition of the name at the caret, and that comment out the
	// selection or its lines.
	GoToDefinition, ToggleComment string
	// Selection, Transform and Folding name the editor's submenus.
	Selection, Transform, Folding string
	// SelectNextOccurrence and SelectAllOccurrences label the editor's
	// items that select the next occurrence of the selection, and all of
	// them.
	SelectNextOccurrence, SelectAllOccurrences string
	// ExpandSelection and SplitIntoLines label the editor's items that
	// grow the selection, and that split a selection of several lines into
	// one selection a line.
	ExpandSelection, SplitIntoLines string
	// UpperCase, LowerCase, TitleCase, SnakeCase, KebabCase, CamelCase and
	// PascalCase label the editor's items that change the case of the
	// selection.
	UpperCase, LowerCase, TitleCase, SnakeCase, KebabCase, CamelCase, PascalCase string
	// SortLines, ReverseLines, UniqueLines, JoinLines and
	// TrimTrailingWhitespace label the editor's items that sort the lines
	// of the selection, reverse them, remove the repeated ones, join them
	// into one, and trim their trailing whitespace.
	SortLines, ReverseLines, UniqueLines, JoinLines, TrimTrailingWhitespace string
	// Fold, Unfold, FoldAll and UnfoldAll label the editor's folding items.
	Fold, Unfold, FoldAll, UnfoldAll string

	// The strings of the diff viewer (package ui/editor).

	// ExpandUp, ExpandDown and ExpandAll label the buttons that show the
	// unchanged lines above a change, below it, and all of them; Expand
	// writes their label from what they show, as in "Expand 4 unmodified
	// lines".
	ExpandUp, ExpandDown, ExpandAll string
	Expand                          func(what string) string
	// UnmodifiedLines writes the count of unchanged lines a separator
	// collapses, as in "4 unmodified lines".
	UnmodifiedLines func(n int) string
	// Revert and Keep label the diff viewer's actions on a change, and
	// HunkAction writes what assistive technology calls a button that
	// takes one of them, as in "Revert change".
	Revert, Keep string
	HunkAction   func(action string) string
	// Line writes the line a gutter button adds a line at, as assistive
	// technology reads it.
	Line func(n int) string
	// NewText and OldText name the sides of a diff viewer: the text as it
	// is, and as it was.
	NewText, OldText string

	// The strings of a find bar.

	// Invalid and NoResults are what a find bar says of a query that is
	// not a valid regular expression, and of one that matches nothing.
	Invalid, NoResults string
	// Matches writes how many of the matches the find bar is at, as "3 of
	// 12", and with more true when it stopped counting them.
	Matches func(current, total int, more bool) string
	// MatchCase, MatchWholeWord, UseRegularExpression and FindInSelection
	// label the find bar's options.
	MatchCase, MatchWholeWord, UseRegularExpression, FindInSelection string
	// PreviousMatch and NextMatch label the find bar's buttons that go
	// through the matches.
	PreviousMatch, NextMatch string
	// Replace, ReplaceAll and Close label the find bar's buttons that
	// replace the match the bar is at, replace every match, and close the
	// bar. Replace also names the field the replacement is typed in while
	// the bar is empty.
	Replace, ReplaceAll, Close string

	// The strings of a go to line bar.

	// GoToLine labels the bar, and LineRange writes what its field shows
	// while it is empty, as in "Line 1 to 120, then :column".
	GoToLine  string
	LineRange func(lines int) string
	// CurrentLine writes where the caret is, as in "Current line 3 of 120,
	// column 5".
	CurrentLine func(line, lines, column int) string
	// Completions labels the editor's list of completions.
	Completions string
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

		CodeEditor: "Code editor",

		GoToDefinition: "Go to Definition", ToggleComment: "Toggle Comment",
		Selection: "Selection", Transform: "Transform", Folding: "Folding",
		SelectNextOccurrence: "Select Next Occurrence", SelectAllOccurrences: "Select All Occurrences",
		ExpandSelection: "Expand Selection", SplitIntoLines: "Split into Lines",
		UpperCase: "Upper Case", LowerCase: "Lower Case", TitleCase: "Title Case",
		SnakeCase: "Snake Case", KebabCase: "Kebab Case", CamelCase: "Camel Case", PascalCase: "Pascal Case",
		SortLines: "Sort Lines", ReverseLines: "Reverse Lines", UniqueLines: "Unique Lines",
		JoinLines: "Join Lines", TrimTrailingWhitespace: "Trim Trailing Whitespace",
		Fold: "Fold", Unfold: "Unfold", FoldAll: "Fold All", UnfoldAll: "Unfold All",

		ExpandUp: "Expand up", ExpandDown: "Expand down", ExpandAll: "Expand all",
		Expand: func(what string) string { return "Expand " + what },
		UnmodifiedLines: func(n int) string {
			word := "lines"
			if n == 1 {
				word = "line"
			}
			return fmt.Sprintf("%d unmodified %s", n, word)
		},
		Revert: "Revert", Keep: "Keep",
		HunkAction: func(action string) string { return action + " change" },
		Line:       func(n int) string { return fmt.Sprintf("Line %d", n) },
		NewText:    "New text", OldText: "Old text",

		Invalid: "Invalid", NoResults: "No results",
		Matches: func(current, total int, more bool) string {
			if more {
				return fmt.Sprintf("%d of %d+", current, total)
			}
			return fmt.Sprintf("%d of %d", current, total)
		},
		MatchCase: "Match case", MatchWholeWord: "Match whole word",
		UseRegularExpression: "Use regular expression", FindInSelection: "Find in selection",
		PreviousMatch: "Previous match", NextMatch: "Next match",
		Replace: "Replace", ReplaceAll: "Replace All", Close: "Close",

		GoToLine:  "Go to line",
		LineRange: func(lines int) string { return fmt.Sprintf("Line 1 to %d, then :column", lines) },
		CurrentLine: func(line, lines, column int) string {
			return fmt.Sprintf("Current line %d of %d, column %d", line, lines, column)
		},
		Completions: "Completions",
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

		CodeEditor: "代码编辑器",

		GoToDefinition: "转到定义", ToggleComment: "切换注释",
		Selection: "选择", Transform: "转换", Folding: "折叠",
		SelectNextOccurrence: "选择下一处匹配", SelectAllOccurrences: "选择所有匹配",
		ExpandSelection: "扩大选择范围", SplitIntoLines: "拆分为多行",
		UpperCase: "转为大写", LowerCase: "转为小写", TitleCase: "转为词首大写",
		SnakeCase: "转为蛇形命名", KebabCase: "转为短横线命名", CamelCase: "转为小驼峰命名", PascalCase: "转为大驼峰命名",
		SortLines: "排序行", ReverseLines: "反转行序", UniqueLines: "去除重复行",
		JoinLines: "合并行", TrimTrailingWhitespace: "删除行尾空白",
		Fold: "折叠", Unfold: "展开", FoldAll: "全部折叠", UnfoldAll: "全部展开",

		ExpandUp: "向上展开", ExpandDown: "向下展开", ExpandAll: "全部展开",
		Expand:          func(what string) string { return "展开 " + what },
		UnmodifiedLines: func(n int) string { return fmt.Sprintf("%d 行未修改", n) },
		Revert:          "还原", Keep: "保留",
		HunkAction: func(action string) string { return action + "更改" },
		Line:       func(n int) string { return fmt.Sprintf("第 %d 行", n) },
		NewText:    "新文本", OldText: "旧文本",

		Invalid: "无效", NoResults: "没有结果",
		Matches: func(current, total int, more bool) string {
			if more {
				return fmt.Sprintf("第 %d 项，共 %d 项以上", current, total)
			}
			return fmt.Sprintf("第 %d 项，共 %d 项", current, total)
		},
		MatchCase: "区分大小写", MatchWholeWord: "匹配整个单词",
		UseRegularExpression: "使用正则表达式", FindInSelection: "在选中范围内查找",
		PreviousMatch: "上一处匹配", NextMatch: "下一处匹配",
		Replace: "替换", ReplaceAll: "全部替换", Close: "关闭",

		GoToLine:  "转到行",
		LineRange: func(lines int) string { return fmt.Sprintf("第 1 到 %d 行，可加 :列", lines) },
		CurrentLine: func(line, lines, column int) string {
			return fmt.Sprintf("第 %d 行，共 %d 行，第 %d 列", line, lines, column)
		},
		Completions: "补全",
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

		CodeEditor: "程式碼編輯器",

		GoToDefinition: "前往定義", ToggleComment: "切換註解",
		Selection: "選取", Transform: "轉換", Folding: "摺疊",
		SelectNextOccurrence: "選取下一處相符項", SelectAllOccurrences: "選取所有相符項",
		ExpandSelection: "擴大選取範圍", SplitIntoLines: "拆分為多行",
		UpperCase: "轉為大寫", LowerCase: "轉為小寫", TitleCase: "轉為標題式大小寫",
		SnakeCase: "轉為蛇形命名", KebabCase: "轉為連字號命名", CamelCase: "轉為小駝峰命名", PascalCase: "轉為大駝峰命名",
		SortLines: "排序行", ReverseLines: "反轉行序", UniqueLines: "移除重複行",
		JoinLines: "合併行", TrimTrailingWhitespace: "刪除行尾空白",
		Fold: "摺疊", Unfold: "展開", FoldAll: "全部摺疊", UnfoldAll: "全部展開",

		ExpandUp: "向上展開", ExpandDown: "向下展開", ExpandAll: "全部展開",
		Expand:          func(what string) string { return "展開 " + what },
		UnmodifiedLines: func(n int) string { return fmt.Sprintf("%d 行未修改", n) },
		Revert:          "還原", Keep: "保留",
		HunkAction: func(action string) string { return action + "變更" },
		Line:       func(n int) string { return fmt.Sprintf("第 %d 行", n) },
		NewText:    "新文字", OldText: "舊文字",

		Invalid: "無效", NoResults: "沒有結果",
		Matches: func(current, total int, more bool) string {
			if more {
				return fmt.Sprintf("第 %d 項，共 %d 項以上", current, total)
			}
			return fmt.Sprintf("第 %d 項，共 %d 項", current, total)
		},
		MatchCase: "區分大小寫", MatchWholeWord: "符合整個單字",
		UseRegularExpression: "使用正規表達式", FindInSelection: "在選取範圍中尋找",
		PreviousMatch: "上一處相符項", NextMatch: "下一處相符項",
		Replace: "取代", ReplaceAll: "全部取代", Close: "關閉",

		GoToLine:  "前往行",
		LineRange: func(lines int) string { return fmt.Sprintf("第 1 到 %d 行，可加 :欄", lines) },
		CurrentLine: func(line, lines, column int) string {
			return fmt.Sprintf("第 %d 行，共 %d 行，第 %d 欄", line, lines, column)
		},
		Completions: "自動完成",
	},
}

// controlTexts are the texts of the controls, in every language the
// framework has them in, as the app changed them. A locale of another
// language says English.
var controlTexts = i18n.New[Texts]("en", builtinTexts)

// textsVersion counts the changes UseTexts made, which the texts an engine
// keeps for its frames are of.
var textsVersion atomic.Uint64

// UseTexts changes the texts the controls say of their own, or adds the
// texts of languages the framework has none of. A language's texts are its
// tag's, as "zh-Hant-TW": its own language's with the changes the app made,
// where a field left at its zero value keeps what the language says.
//
//	ui.UseTexts(map[string]ui.Texts{
//		"zh-Hans": {NextMonth: "下一月"},
//		"de":      {Find: "Suchen", Search: "Suchen", Clear: "Löschen"},
//	})
//
// It is safe from any goroutine, makes its changes at once, and the windows
// shown draw their controls again with the texts it set.
func UseTexts(texts map[string]Texts) {
	controlTexts.Change(texts)
	textsVersion.Add(1)
	textsChanged()
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

// ownTexts returns the texts of the controls in the language of the locale
// the app shows, which the engine keeps until the locale or the texts
// change: the controls read them as they build, every frame.
func (rt *engine) ownTexts() *Texts {
	locale, version := rt.host.locale(), textsVersion.Load()
	if rt.textsOf == nil || rt.textsLocale != locale || rt.textsVersion != version {
		rt.textsOf, rt.textsLocale, rt.textsVersion = textsFor(locale), locale, version
	}
	return rt.textsOf
}

// Texts returns the texts the controls and the code editor say of their own
// in the language of the app's locale (mygo.App.SetLocale), as the app
// changed them with UseTexts. A widget of the app that says the framework's
// own words, as a code editor built on package ui/editor does, reads them
// here. They are the framework's own: read them, and change them with
// UseTexts.
func (c *Context) Texts() *Texts {
	if c == nil || c.rt == nil {
		return textsFor("")
	}
	return c.rt.ownTexts()
}
