// Package i18n matches the languages an app has to the locale of the
// desktop, and formats values for it: a count with the plural of the
// language, a number with its separators and its digits, a date and a time
// of day with its patterns.
//
// MyGo itself has its own strings, of the controls package ui draws, in the
// languages App.SetLocale speaks; an app's own strings are its own. Match
// tells which of the languages an app has the desktop's locale asks for.
package i18n

import "strings"

// Match returns the tag among available that best matches locale, a language
// tag such as "zh-Hant-HK" or a POSIX locale such as "zh_TW.UTF-8" or
// "sr_RS@latin". It compares the language of the two, then their script,
// then their region: the same language, script and region, else the same
// language and script, else the same language and region, else the same
// language, else another variant of the language, as pt-PT gets pt-BR. A
// Hong Kong locale therefore takes the Traditional Chinese of Taiwan,
// zh-TW, and the other way around.
//
// The scripts of Chinese are not variants of each other: zh-TW does not
// match zh-Hans. Nor are the Spanish of Latin America and the Spanish of
// Spain: es-419 does not match es-ES.
//
// It returns "" when none matches, and the caller falls back to the language
// it writes its own strings in.
func Match(locale string, available []string) string {
	lang, script, region := split(locale)
	if lang == "" {
		return ""
	}
	best, bestScore := "", -1
	for _, a := range available {
		aLang, aScript, aRegion := split(a)
		if aLang != lang {
			continue
		}
		// One script of Chinese is no fallback for the other, and a tag
		// that names none is Simplified.
		if lang == "zh" && !strings.EqualFold(aScript, script) {
			continue
		}
		// The Spanish of Latin America and the Spanish of Spain are not
		// variants of each other. A tag that names no region is the
		// language itself, and suits every region of it.
		if lang == "es" && region != "" && aRegion != "" && !strings.EqualFold(aRegion, region) {
			continue
		}
		score := regionScore(region, aRegion)
		switch {
		case strings.EqualFold(aScript, script):
			score += scriptSame
		case aScript == "":
			// The tag names no script, and suits the one asked for.
			score += scriptUnknown
		}
		if score > bestScore {
			best, bestScore = a, score
		}
	}
	return best
}

// The score of a tag that suits the locale: the script of the two counts for
// more than their region, as Match says the order is.
const (
	scriptSame    = 20
	scriptUnknown = 10
)

// regionScore scores the region of a tag that suits the locale: the same
// one, none named, or another.
func regionScore(region, aRegion string) int {
	switch {
	case strings.EqualFold(aRegion, region):
		return 2 // the same region, or neither names one
	case aRegion == "":
		return 1 // the language itself, without a region
	}
	return 0
}

// BaseLanguage returns the language of a tag: "zh-Hant" is "zh".
func BaseLanguage(tag string) string {
	lang, _, _ := split(tag)
	return lang
}

// split parses a language tag, or a POSIX locale, into its language, script
// and region: "zh_Hant_TW.UTF-8" is "zh", "Hant" and "TW", and the
// "sr_RS@latin" of a POSIX locale is "sr", "Latn" and "RS". Chinese without
// a script is Simplified, except in the regions that write Traditional.
func split(locale string) (lang, script, region string) {
	locale, _, _ = strings.Cut(locale, ".") // zh_TW.UTF-8
	locale, modifier, _ := strings.Cut(locale, "@")
	parts := strings.FieldsFunc(locale, func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return "", "", ""
	}
	lang = strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		switch {
		case len(p) == 4:
			script = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		case len(p) == 2 || len(p) == 3 && p[0] >= '0' && p[0] <= '9':
			region = strings.ToUpper(p)
		}
	}
	if script == "" {
		script = modifierScript(modifier)
	}
	if lang == "zh" && script == "" {
		script = "Hans"
		switch region {
		case "TW", "HK", "MO":
			script = "Hant"
		}
	}
	return lang, script, region
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
