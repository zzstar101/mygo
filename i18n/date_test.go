package i18n

import (
	"strings"
	"testing"
	"time"
)

// thursday is 5 March 2026, at 17:04 and 9 seconds.
var thursday = time.Date(2026, time.March, 5, 17, 4, 9, 0, time.UTC)

func TestDate(t *testing.T) {
	for _, tc := range []struct {
		locale string
		style  Style
		want   string
	}{
		{"en", Short, "3/5/26"},
		{"en", Medium, "Mar 5, 2026"},
		{"en", Long, "Thursday, March 5, 2026"},
		{"en-US", Long, "Thursday, March 5, 2026"},
		{"de", Short, "05.03.26"},
		{"de", Medium, "05.03.2026"},
		{"de", Long, "Donnerstag, 5. März 2026"},
		{"fr", Short, "05/03/2026"},
		{"fr", Medium, "5 mars 2026"},
		{"fr", Long, "jeudi 5 mars 2026"},
		{"es", Long, "jueves, 5 de marzo de 2026"},
		{"pt", Long, "quinta-feira, 5 de março de 2026"},
		{"pt-BR", Long, "quinta-feira, 5 de março de 2026"},
		{"it", Long, "giovedì 5 marzo 2026"},
		{"nl", Short, "05-03-2026"},
		{"nl", Long, "donderdag 5 maart 2026"},
		{"ru", Medium, "5 мар. 2026 г."},
		{"ru", Long, "четверг, 5 марта 2026 г."},
		{"pl", Long, "czwartek, 5 marca 2026"},
		{"tr", Long, "5 Mart 2026 Perşembe"},
		{"ar", Short, "٥/٣/٢٠٢٦"},
		{"ar", Long, "الخميس، ٥ مارس ٢٠٢٦"},
		{"he", Short, "5.3.2026"},
		{"he", Medium, "5 במרץ 2026"},
		{"hi", Long, "गुरुवार, 5 मार्च 2026"},
		{"ja", Short, "2026/03/05"},
		{"ja", Long, "2026年3月5日木曜日"},
		{"ko", Medium, "2026. 3. 5."},
		{"ko", Long, "2026년 3월 5일 목요일"},
		{"zh-Hans", Medium, "2026年3月5日"},
		{"zh-Hans", Long, "2026年3月5日星期四"},
		{"zh-Hant", Long, "2026年3月5日星期四"},
		// Chinese of Taiwan is Chinese of the traditional script.
		{"zh-TW", Long, "2026年3月5日星期四"},
		{"zh_Hant_HK", Long, "2026年3月5日星期四"},
		// A language i18n has no data for keeps to ISO, whatever the style.
		{"sv", Short, "2026-03-05"},
		{"sv", Long, "2026-03-05"},
		{"", Medium, "2026-03-05"},
		// The POSIX spellings of the languages i18n knows.
		{"de_DE.UTF-8", Medium, "05.03.2026"},
		{"zh_CN.UTF-8", Long, "2026年3月5日星期四"},
	} {
		if got := Date(tc.locale, thursday, tc.style); got != tc.want {
			t.Errorf("Date(%q, Thursday, %d) = %q, want %q", tc.locale, tc.style, got, tc.want)
		}
	}
}

func TestTime(t *testing.T) {
	morning := time.Date(2026, time.March, 5, 9, 4, 0, 0, time.UTC)
	midnight := time.Date(2026, time.March, 5, 0, 4, 0, 0, time.UTC)
	noon := time.Date(2026, time.March, 5, 12, 4, 0, 0, time.UTC)
	for _, tc := range []struct {
		locale string
		t      time.Time
		want   string
	}{
		{"en", thursday, "5:04 PM"},
		{"en", morning, "9:04 AM"},
		{"en", midnight, "12:04 AM"},
		{"en", noon, "12:04 PM"},
		{"de", thursday, "17:04"},
		{"fr", thursday, "17:04"},
		{"it", thursday, "17:04"},
		{"nl", thursday, "17:04"},
		{"ru", thursday, "17:04"},
		{"pl", thursday, "17:04"},
		{"tr", thursday, "17:04"},
		{"ja", thursday, "17:04"},
		{"es", thursday, "17:04"},
		{"pt", thursday, "17:04"},
		{"hi", thursday, "5:04 pm"},
		{"ar", thursday, "٥:٠٤ م"},
		{"ar", morning, "٩:٠٤ ص"},
		{"ko", thursday, "오후 5:04"},
		{"ko", morning, "오전 9:04"},
		{"zh-Hans", thursday, "下午5:04"},
		{"zh-Hans", morning, "上午9:04"},
		{"zh-Hant", thursday, "下午5:04"},
		{"zh-TW", morning, "上午9:04"},
		// A language i18n has no data for keeps to the 24 hour clock.
		{"sv", thursday, "17:04"},
		{"", thursday, "17:04"},
		{"de_DE.UTF-8", thursday, "17:04"},
	} {
		if got := Time(tc.locale, tc.t); got != tc.want {
			t.Errorf("Time(%q, %s) = %q, want %q", tc.locale, tc.t.Format(time.RFC3339), got, tc.want)
		}
	}
}

// TestEveryLanguageWritesDates checks that every language writes a whole date
// and a whole time, in its own digits, whatever the style.
func TestEveryLanguageWritesDates(t *testing.T) {
	for _, tag := range Languages() {
		for _, style := range []Style{Short, Medium, Long} {
			got := Date(tag, thursday, style)
			if got == "" {
				t.Errorf("Date(%q, Thursday, %d) is empty", tag, style)
			}
			// The styles with a year of four digits write it in the
			// digits of the language.
			if style != Short && !strings.Contains(got, langs[tag].digitsOf("2026")) {
				t.Errorf("Date(%q, Thursday, %d) = %q, without the year", tag, style, got)
			}
		}
		got := Time(tag, thursday)
		if got == "" || !strings.Contains(got, langs[tag].digitsOf("04")) {
			t.Errorf("Time(%q, Thursday) = %q, without the minute", tag, got)
		}
	}
}

func TestDateFormat(t *testing.T) {
	l, ok := languageOf("en")
	if !ok {
		t.Fatal("en is not a language")
	}
	for _, tc := range []struct {
		pattern string
		want    string
	}{
		{"yyyy-MM-dd", "2026-03-05"},
		{"yy-M-d", "26-3-5"},
		{"EEEE, MMMM d", "Thursday, March 5"},
		{"MMM", "Mar"},
		{"H:mm:ss", "17:04:09"},
		{"h:mm a", "5:04 PM"},
		// A quoted part is literal text, and a letter of its own.
		{"d 'of' MMMM", "5 of March"},
		{"h 'o''clock'", "5 o'clock"},
		{"'y'yy", "y26"},
		// A letter the pattern does not know is literal text.
		{"d of MMMM", "5 of March"},
		{"q d", "q 5"},
	} {
		if got := formatDate(tc.pattern, thursday, l); got != tc.want {
			t.Errorf("formatDate(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
	zh, ok := languageOf("zh-Hans")
	if !ok {
		t.Fatal("zh-Hans is not a language")
	}
	// Two letters of their own in a row are two fields, as Chinese writes
	// "ah:mm" for "下午5:04".
	if got := formatDate("ah:mm", thursday, zh); got != "下午5:04" {
		t.Errorf("formatDate(ah:mm) = %q, want 下午5:04", got)
	}
}

func TestStyleOfDate(t *testing.T) {
	// A style i18n does not know is the medium one.
	if got, want := Date("en", thursday, Style(200)), Date("en", thursday, Medium); got != want {
		t.Errorf("an unknown style writes %q, want %q", got, want)
	}
}
