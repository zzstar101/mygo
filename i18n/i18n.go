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
// tag such as "zh-Hant-HK" or a POSIX locale such as "zh_TW.UTF-8": the same
// language, script and region, else the same language and script, else the
// same language, else another variant of the language, as pt-PT gets pt-BR.
// The scripts of Chinese are not variants of each other: zh-TW does not
// match zh-Hans.
//
// It returns "" when none matches, and the caller falls back to the language
// it writes its own strings in.
func Match(locale string, available []string) string {
	lang, script, region := split(locale)
	if lang == "" {
		return ""
	}
	var candidates []string
	if script != "" {
		if region != "" {
			candidates = append(candidates, lang+"-"+script+"-"+region)
		}
		candidates = append(candidates, lang+"-"+script)
	}
	if region != "" {
		candidates = append(candidates, lang+"-"+region)
	}
	candidates = append(candidates, lang)
	for _, c := range candidates {
		for _, a := range available {
			if strings.EqualFold(a, c) {
				return a
			}
		}
	}
	// Another variant of the language, as pt-PT gets pt-BR. Chinese
	// scripts are not variants of each other.
	if lang != "zh" {
		for _, a := range available {
			if BaseLanguage(a) == lang {
				return a
			}
		}
	}
	return ""
}

// BaseLanguage returns the language of a tag: "zh-Hant" is "zh".
func BaseLanguage(tag string) string {
	lang, _, _ := split(tag)
	return lang
}

// split parses a language tag, or a POSIX locale, into its language, script
// and region: "zh_Hant_TW.UTF-8" is "zh", "Hant" and "TW". Chinese without a
// script is Simplified, except in the regions that write Traditional.
func split(locale string) (lang, script, region string) {
	locale, _, _ = strings.Cut(locale, ".") // zh_TW.UTF-8
	locale, _, _ = strings.Cut(locale, "@") // sr_RS@latin
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
	if lang == "zh" && script == "" {
		script = "Hans"
		switch region {
		case "TW", "HK", "MO":
			script = "Hant"
		}
	}
	return lang, script, region
}
