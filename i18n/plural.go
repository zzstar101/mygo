package i18n

import "strings"

// Plural is a plural category of a language: the forms a language writes a
// count in. English has two of them, One and Other; Arabic has all six, and
// Chinese only Other.
type Plural uint8

// Plural categories, as Unicode CLDR names them.
const (
	// Zero is the form of zero, as in Arabic.
	Zero Plural = iota
	// One is the form of one, as in "1 file".
	One
	// Two is the form of two, as in Arabic and Hebrew.
	Two
	// Few is the form of a few, as in Russian's "2 файла".
	Few
	// Many is the form of many, as in Russian's "5 файлов".
	Many
	// Other is the form of everything else, which every language has.
	Other
)

// String returns the name of the category, as CLDR names it: "zero", "one",
// "two", "few", "many" or "other".
func (p Plural) String() string {
	switch p {
	case Zero:
		return "zero"
	case One:
		return "one"
	case Two:
		return "two"
	case Few:
		return "few"
	case Many:
		return "many"
	case Other:
		return "other"
	}
	return "other"
}

// Forms are the ways a language writes a count, one per plural category:
// language selects the one that suits the count, and writes it with the count
// in the form's %d.
//
// A language without a category leaves its form empty, and so does an app
// that writes the same words for it: Select falls back to Other, and to One
// when Other is empty too.
type Forms struct {
	Zero  string
	One   string
	Two   string
	Few   string
	Many  string
	Other string
}

// form returns the form of the category, empty when the language has none.
func (f Forms) form(p Plural) string {
	switch p {
	case Zero:
		return f.Zero
	case One:
		return f.One
	case Two:
		return f.Two
	case Few:
		return f.Few
	case Many:
		return f.Many
	}
	return f.Other
}

// PluralOf returns the plural category of n in the language of locale, which
// an app picks its own words with:
//
//	switch i18n.PluralOf(locale, n) {
//	case i18n.One:
//		return "1 file"
//	}
//
// A language i18n has no data for has Other alone.
func PluralOf(locale string, n int) Plural {
	l, ok := languageOf(locale)
	if !ok || l.plural == nil {
		return Other
	}
	if n < 0 {
		n = -n
	}
	return l.plural(n)
}

// Select returns the form of n that the language of locale uses, with n
// written the way that language writes numbers in its %d: French gets
// "1 234 fichiers", Arabic "١٢٣٤ ملفًا" and Hindi "12,34,567 फ़ाइलें".
//
// The count is the form's %d, and %% is a literal percent sign; other verbs
// are left as they are.
func Select(locale string, n int, forms Forms) string {
	form := forms.form(PluralOf(locale, n))
	if form == "" {
		form = forms.Other
	}
	if form == "" {
		form = forms.One
	}
	form = strings.ReplaceAll(form, "%d", Number(locale, n))
	return strings.ReplaceAll(form, "%%", "%")
}

// The plural rules of the languages i18n knows, as CLDR states them for
// counts without a fraction.

// pluralOneOther is the rule of English and of the languages like it.
func pluralOneOther(n int) Plural {
	if n == 1 {
		return One
	}
	return Other
}

// pluralZeroOne is the rule of French and Portuguese, where zero is one too.
func pluralZeroOne(n int) Plural {
	if n <= 1 {
		return One
	}
	return Other
}

// pluralOnlyOther is the rule of Chinese, Japanese and Korean, which write a
// count the same way whatever it is.
func pluralOnlyOther(int) Plural { return Other }

// pluralHindi is the rule of Hindi, where zero is one too.
func pluralHindi(n int) Plural {
	if n == 0 || n == 1 {
		return One
	}
	return Other
}

// pluralRussian is the rule of Russian and of the languages like it.
func pluralRussian(n int) Plural {
	switch {
	case n%10 == 1 && n%100 != 11:
		return One
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return Few
	case n%10 == 0 || n%10 >= 5 && n%10 <= 9 || n%100 >= 11 && n%100 <= 14:
		return Many
	}
	return Other
}

// pluralPolish is the rule of Polish.
func pluralPolish(n int) Plural {
	switch {
	case n == 1:
		return One
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return Few
	case n%10 <= 1 || n%10 >= 5 && n%10 <= 9 || n%100 >= 12 && n%100 <= 14:
		return Many
	}
	return Other
}

// pluralArabic is the rule of Arabic, with all six categories.
func pluralArabic(n int) Plural {
	switch rem := n % 100; {
	case n == 0:
		return Zero
	case n == 1:
		return One
	case rem == 2:
		return Two
	case rem >= 3 && rem <= 10:
		return Few
	case rem >= 11 && rem <= 99:
		return Many
	}
	return Other
}

// pluralHebrew is the rule of Hebrew.
func pluralHebrew(n int) Plural {
	switch {
	case n == 1:
		return One
	case n == 2:
		return Two
	case n != 0 && n%10 == 0:
		return Many
	}
	return Other
}
