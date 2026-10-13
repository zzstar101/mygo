package i18n

import (
	"slices"
	"strings"
	"testing"
)

func TestPluralOf(t *testing.T) {
	for _, tc := range []struct {
		locale string
		n      int
		want   Plural
	}{
		// One and Other.
		{"en", 0, Other}, {"en", 1, One}, {"en", 2, Other},
		{"en-US", 1, One}, {"de", 1, One}, {"de-DE", 2, Other},
		{"es", 1, One}, {"it", 1, One}, {"nl", 1, One}, {"tr", 5, Other},
		// French and Portuguese count zero as one.
		{"fr", 0, One}, {"fr", 1, One}, {"fr", 2, Other},
		{"pt", 0, One}, {"pt-BR", 1, One}, {"pt-BR", 2, Other},
		// Languages that write every count the same way.
		{"zh-Hans", 1, Other}, {"zh-Hant-HK", 100, Other},
		{"ja", 1, Other}, {"ko", 3, Other},
		// Hindi counts zero as one.
		{"hi", 0, One}, {"hi", 1, One}, {"hi", 2, Other},
		// Russian and Polish.
		{"ru", 0, Many}, {"ru", 1, One}, {"ru", 2, Few}, {"ru", 5, Many},
		{"ru", 11, Many}, {"ru", 21, One}, {"ru", 101, One}, {"ru", 102, Few},
		{"pl", 0, Many}, {"pl", 1, One}, {"pl", 2, Few}, {"pl", 5, Many},
		{"pl", 12, Many}, {"pl", 22, Few}, {"pl", 25, Many},
		// Arabic and Hebrew, with their two.
		{"ar", 0, Zero}, {"ar", 1, One}, {"ar", 2, Two}, {"ar", 3, Few},
		{"ar", 10, Few}, {"ar", 11, Many}, {"ar", 99, Many}, {"ar", 100, Other},
		{"ar-EG", 103, Few}, {"ar", 102, Other}, {"ar", 202, Other},
		{"he", 1, One}, {"he", 2, Two}, {"he", 3, Other}, {"he", 10, Other},
		{"he", 20, Other}, {"he", 200, Other},
		// A language i18n has no data for, and the POSIX spellings.
		{"sv", 1, Other}, {"", 1, Other}, {"C", 2, Other},
		{"de_DE.UTF-8", 1, One}, {"zh_TW.UTF-8", 1, Other},
	} {
		if got := PluralOf(tc.locale, tc.n); got != tc.want {
			t.Errorf("PluralOf(%q, %d) = %s, want %s", tc.locale, tc.n, got, tc.want)
		}
	}
	// A negative count is the category of its size.
	if got := PluralOf("en", -1); got != One {
		t.Errorf("PluralOf(en, -1) = %s, want one", got)
	}
	if got := PluralOf("ru", -2); got != Few {
		t.Errorf("PluralOf(ru, -2) = %s, want few", got)
	}
}

func TestPluralString(t *testing.T) {
	for plural, want := range map[Plural]string{
		Zero: "zero", One: "one", Two: "two", Few: "few", Many: "many", Other: "other",
		Plural(200): "other",
	} {
		if got := plural.String(); got != want {
			t.Errorf("Plural(%d) is %q", plural, got)
		}
	}
}

func TestSelect(t *testing.T) {
	files := Forms{One: "%d file", Other: "%d files"}
	for _, tc := range []struct {
		locale string
		n      int
		forms  Forms
		want   string
	}{
		{"en", 1, files, "1 file"},
		{"en", 0, files, "0 files"},
		{"en", 2, files, "2 files"},
		{"en", 1234567, files, "1,234,567 files"},
		// The count is written the way the language writes numbers.
		{"de", 1234, files, "1.234 files"},
		{"fr", 1234, files, "1\u202f234 files"},
		{"hi", 1234567, files, "12,34,567 files"},
		{"ar", 2, Forms{Two: "%d ملفان"}, "٢ ملفان"},
		// A form the language has no word for falls back to Other, then One.
		{"ru", 1, Forms{Other: "%d файлов"}, "1 файлов"},
		{"ar", 3, Forms{One: "%d ملف"}, "٣ ملف"},
		{"en", 1, Forms{}, ""},
		// %% is a literal percent sign.
		{"en", 1, Forms{One: "%d%%"}, "1%"},
		{"en", 2, Forms{Other: "%d%% of %d"}, "2% of 2"},
	} {
		if got := Select(tc.locale, tc.n, tc.forms); got != tc.want {
			t.Errorf("Select(%q, %d, %+v) = %q, want %q", tc.locale, tc.n, tc.forms, got, tc.want)
		}
	}
}

// TestLanguageData checks that every language i18n knows has everything the
// helpers need, and that none of them panics on any kind of count.
func TestLanguageData(t *testing.T) {
	want := []string{
		"ar", "de", "en", "es", "fr", "he", "hi", "it", "ja", "ko",
		"nl", "pl", "pt", "ru", "tr", "zh-Hans", "zh-Hant",
	}
	if got := Languages(); !slices.Equal(got, want) {
		t.Fatalf("Languages() = %q, want %q", got, want)
	}
	counts := []int{0, 1, 2, 3, 5, 10, 11, 21, 99, 100, 101, 1000}
	for _, tag := range Languages() {
		l, ok := languageOf(tag)
		if !ok {
			t.Fatalf("%s is not a language", tag)
		}
		if l.plural == nil {
			t.Errorf("%s has no plural rule", tag)
		}
		if l.decimal == "" || l.group == "" || len(l.groupSizes) == 0 {
			t.Errorf("%s does not say how it writes numbers", tag)
		}
		if l.dateShort == "" || l.dateMedium == "" || l.dateLong == "" || l.time == "" {
			t.Errorf("%s does not say how it writes dates", tag)
		}
		for _, months := range [][12]string{l.monthsShort, l.monthsLong} {
			for i, name := range months {
				if name == "" {
					t.Errorf("%s has no name for month %d", tag, i+1)
				}
			}
		}
		for i, name := range l.weekdays {
			if name == "" {
				t.Errorf("%s has no name for day %d", tag, i)
			}
		}
		forms := Forms{Zero: "zero %d", One: "one %d", Two: "two %d", Few: "few %d", Many: "many %d", Other: "other %d"}
		for _, n := range counts {
			got := Select(tag, n, forms)
			if !strings.HasPrefix(got, PluralOf(tag, n).String()) {
				t.Errorf("Select(%q, %d, every form) = %q, want a form of %s", tag, n, got, PluralOf(tag, n))
			}
		}
	}
}
