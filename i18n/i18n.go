// Package i18n matches the languages an app has to the locale the desktop
// asks for, and keeps the texts of an app in each of them.
//
// MyGo itself has its own strings, of the menus and of the controls
// package ui draws, in the languages App.SetLocale speaks; an app's own
// strings are its own, which a Catalog keeps. Match tells which of the
// languages an app has the desktop's locale asks for. Writing counts,
// numbers and dates the way a language does is the work of a library of
// the Unicode CLDR, such as golang.org/x/text, or of Intl in a page.
package i18n

import "strings"

// Match returns the tag among available that best matches locale, a language
// tag such as "zh-Hant-HK" or a POSIX locale such as "zh_TW.UTF-8" or
// "sr_RS@latin". It compares the language of the two, then their script,
// then their region: the same language, script and region, else the same
// language and script, else the same language and a region that holds the
// other (es-419, the Spanish of Latin America, for es-MX), else the
// language without a region, else another variant of the language, as
// es-MX gets es-ES and pt-PT gets pt-BR. A Hong Kong locale therefore takes
// the Traditional Chinese of Taiwan, zh-TW, and the other way around.
//
// The scripts of Chinese are not variants of each other: zh-TW does not
// match zh-Hans. The old codes of a language are the language: iw is he,
// in is id, and no, Norwegian, is nb, Bokmål.
//
// It returns "" when none matches, and the caller falls back to the language
// it writes its own strings in.
func Match(locale string, available []string) string {
	want := parse(locale)
	if want.lang == "" {
		return ""
	}
	best, bestScore := "", -1
	for _, a := range available {
		if score := want.score(parse(a)); score > bestScore {
			best, bestScore = a, score
		}
	}
	return best
}

// BaseLanguage returns the language of a tag, lowercased, with an old code
// read as the language's code: "zh-Hant" is "zh", and "iw-IL" is "he".
func BaseLanguage(tag string) string {
	return parse(tag).lang
}

// tag is a language tag, or a POSIX locale, taken apart: its language,
// lowercased and with an old code read as the language's, and its script
// and region as written. Chinese without a script is Simplified, except
// in the regions that write Traditional.
type tag struct {
	lang, script, region string
}

// The score of a tag that suits the locale: the script of the two counts for
// more than their region, as Match says the order is.
const (
	scriptSame    = 20
	scriptUnknown = 10
)

// score returns how well the tag a suits t, or -1 when it does not.
func (t tag) score(a tag) int {
	if a.lang != t.lang {
		return -1
	}
	// One script of Chinese is no fallback for the other.
	if t.lang == "zh" && !strings.EqualFold(a.script, t.script) {
		return -1
	}
	score := regionScore(t.region, a.region)
	switch {
	case strings.EqualFold(a.script, t.script):
		score += scriptSame
	case a.script == "":
		// The tag names no script, and suits the one asked for.
		score += scriptUnknown
	}
	return score
}

// regionScore scores the region of a tag that suits the locale: the same
// one, one that holds the other, none named, or another.
func regionScore(region, aRegion string) int {
	switch {
	case strings.EqualFold(aRegion, region):
		return 3 // the same region, or neither names one
	case holds(aRegion, region) || holds(region, aRegion):
		return 2 // es-419 for es-MX, or es-MX for es-419
	case aRegion == "":
		return 1 // the language itself, without a region
	}
	return 0
}

// holds reports whether the region outer holds the country inner: 419,
// Latin America, holds the countries of the Spanish of Latin America, as
// the Unicode CLDR has es-419 the parent of their Spanish.
func holds(outer, inner string) bool {
	if outer != "419" || len(inner) != 2 {
		return false
	}
	switch strings.ToUpper(inner) {
	case "AR", "BO", "BR", "BZ", "CL", "CO", "CR", "CU", "DO", "EC", "GT", "HN",
		"MX", "NI", "PA", "PE", "PR", "PY", "SV", "US", "UY", "VE":
		return true
	}
	return false
}

// aliases are the old codes of languages, and the codes a desktop may
// report for one the other code names.
var aliases = map[string]string{
	"iw": "he",  // Hebrew
	"in": "id",  // Indonesian
	"ji": "yi",  // Yiddish
	"jw": "jv",  // Javanese
	"mo": "ro",  // Moldavian is Romanian
	"no": "nb",  // Norwegian, as Bokmål
	"tl": "fil", // Tagalog, as Filipino
}

// parse takes a language tag, or a POSIX locale, apart: "zh_Hant_TW.UTF-8"
// is "zh", "Hant" and "TW", and the "sr_RS@latin" of a POSIX locale is "sr",
// "Latn" and "RS".
func parse(locale string) tag {
	locale, _, _ = strings.Cut(locale, ".") // zh_TW.UTF-8
	locale, modifier, _ := strings.Cut(locale, "@")
	var t tag
	first := true
	for locale != "" {
		var p string
		if i := strings.IndexAny(locale, "-_"); i >= 0 {
			p, locale = locale[:i], locale[i+1:]
		} else {
			p, locale = locale, ""
		}
		switch {
		case p == "":
		case first:
			t.lang = strings.ToLower(p)
			if a, ok := aliases[t.lang]; ok {
				t.lang = a
			}
			first = false
		case len(p) == 4 && t.script == "" && t.region == "":
			t.script = p
		case t.region == "" && (len(p) == 2 || len(p) == 3 && p[0] >= '0' && p[0] <= '9'):
			t.region = p
		}
	}
	if t.lang == "" || t.lang == "c" || t.lang == "posix" {
		return tag{}
	}
	if t.script == "" {
		t.script = modifierScript(modifier)
	}
	if t.lang == "zh" && t.script == "" {
		t.script = "Hans"
		switch strings.ToUpper(t.region) {
		case "TW", "HK", "MO":
			t.script = "Hant"
		}
	}
	return t
}

// modifierScript returns the script a POSIX modifier names: the "latin" of
// sr_RS@latin is Latn, which a language tag such as sr-RS leaves out.
func modifierScript(modifier string) string {
	switch strings.ToLower(modifier) {
	case "latin":
		return "Latn"
	case "cyrillic":
		return "Cyrl"
	case "arabic":
		return "Arab"
	}
	return ""
}
