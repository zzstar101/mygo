package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The languages of a build, as each platform names them: a language tag
// names one language, and every system writes it its own way.

// langEnUS is the language of the resources of an app that declares no
// language, which is the one every Windows there is knows.
const langEnUS = 0x0409

// desktopTag is the tag of a localized key of a desktop entry, which uses
// underscores: zh-Hans is zh_Hans and pt-BR is pt_BR.
func desktopTag(tag string) string {
	return strings.ReplaceAll(tag, "-", "_")
}

// baseLanguage is the language of a tag, lowercased: zh-Hant-HK is zh.
func baseLanguage(tag string) string {
	base, _, _ := strings.Cut(tag, "-")
	return strings.ToLower(base)
}

// scriptLanguage is the language and script of a tag, lowercased, which
// tells apart the languages one script each writes: zh-Hant-HK is zh-hant
// and zh-Hans-CN is zh-hans.
func scriptLanguage(tag string) string {
	parts := strings.Split(tag, "-")
	if len(parts) > 1 && isScript(parts[1]) {
		return strings.ToLower(parts[0]) + "-" + strings.ToLower(parts[1])
	}
	return strings.ToLower(parts[0])
}

// isScript reports whether a part of a tag names a script, as "Hans" and
// "Hant": four letters, where a region is two letters or three digits.
func isScript(part string) bool {
	if len(part) != 4 {
		return false
	}
	for _, r := range part {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// windowsLanguage is one language of a Windows resource: the ID Windows
// names the language by, and the app's name in it.
type windowsLanguage struct {
	id   uint16
	name string
}

// windowsLanguages returns the languages of the Windows resources of the
// app: the languages it declares that Windows knows, in the order it
// declares them, or en-US alone with the app's own name when it declares
// none.
func (c *Config) windowsLanguages() []windowsLanguage {
	var langs []windowsLanguage
	for _, l := range c.Languages {
		id, ok := windowsLanguageID(l.Tag)
		if !ok {
			warnf("The app has no %s file properties: Windows does not know the language", l.Tag)
			continue
		}
		name := l.Name
		if name == "" {
			name = c.Name
		}
		langs = append(langs, windowsLanguage{id: id, name: name})
	}
	if len(langs) == 0 {
		langs = append(langs, windowsLanguage{id: langEnUS, name: c.Name})
	}
	return langs
}

// windowsLanguageID returns the ID of the language of a tag, which names
// the language of a Windows resource, as 0x0409 for en-US, and whether
// Windows knows the language.
func windowsLanguageID(tag string) (uint16, bool) {
	for _, key := range []string{strings.ToLower(tag), scriptLanguage(tag), baseLanguage(tag)} {
		if id, ok := windowsLanguages[key]; ok {
			return id, true
		}
	}
	return 0, false
}

// nsisLanguage returns the name of the NSIS language file of a tag, which
// MUI_LANGUAGE takes without its extension, as "SimpChinese", and whether
// NSIS has a language file for it at all.
func nsisLanguage(tag string) (string, bool) {
	for _, key := range []string{strings.ToLower(tag), scriptLanguage(tag), baseLanguage(tag)} {
		if name, ok := nsisLanguages[key]; ok {
			return name, true
		}
	}
	return "", false
}

// nsisLanguageMacros returns the lines that give the Windows installer the
// languages of the app, one MUI_LANGUAGE macro each, which the installer
// picks among by the language of the system. It is English alone for an app
// that declares no language, or one whose languages NSIS does not know.
//
// A language file that is not there fails the build, so the language files
// of the NSIS installation of tool are checked where they can be found.
func nsisLanguageMacros(c *Config, tool string) string {
	files := nsisLanguageFiles(tool)
	seen := map[string]bool{}
	var b strings.Builder
	for _, l := range c.Languages {
		name, ok := nsisLanguage(l.Tag)
		if !ok {
			warnf("The Windows installer has no %s language: NSIS does not know it", l.Tag)
			continue
		}
		if files != "" && !fileExists(filepath.Join(files, name+".nlf")) {
			warnf("The Windows installer has no %s language: NSIS has no %s.nlf", l.Tag, name)
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		fmt.Fprintf(&b, "!insertmacro MUI_LANGUAGE %s\n", nsisString(name))
	}
	if b.Len() == 0 {
		return "!insertmacro MUI_LANGUAGE \"English\"\n"
	}
	return b.String()
}

// nsisLanguageFiles returns the directory of the language files of the NSIS
// installation makensis belongs to, or "" when they are not where it keeps
// them.
func nsisLanguageFiles(tool string) string {
	dir := filepath.Dir(tool)
	for _, p := range []string{
		filepath.Join(dir, "Contrib", "Language files"),                        // an installed NSIS
		filepath.Join(dir, "..", "Contrib", "Language files"),                  // the downloaded NSIS, whose makensis is in Bin
		filepath.Join(dir, "..", "share", "nsis", "Contrib", "Language files"), // a package, as Homebrew
		filepath.Join(dir, "..", "..", "share", "nsis", "Contrib", "Language files"),
	} {
		if isDir(p) {
			return p
		}
	}
	return ""
}

// windowsLanguages are the languages Windows names by an ID: the language
// and region of the tags of a system, with the region of the language
// alone where Windows has one.
var windowsLanguages = map[string]uint16{
	"af": 0x0436, "am": 0x045E, "ar": 0x0401, "ar-eg": 0x0C01, "ar-sa": 0x0401,
	"az": 0x042C, "az-latn": 0x042C, "ba": 0x046D, "be": 0x0423, "bg": 0x0402,
	"bn": 0x0445, "bs": 0x141A, "ca": 0x0403, "co": 0x0483, "cs": 0x0405,
	"cy": 0x0452, "da": 0x0406, "de": 0x0407, "de-at": 0x0C07, "de-ch": 0x0807,
	"de-de": 0x0407, "el": 0x0408, "en": 0x0409, "en-au": 0x0C09, "en-ca": 0x1009,
	"en-gb": 0x0809, "en-ie": 0x1809, "en-in": 0x4009, "en-nz": 0x1409, "en-sg": 0x4809,
	"en-us": 0x0409, "en-za": 0x1C09, "es": 0x040A, "es-ar": 0x2C0A, "es-cl": 0x340A,
	"es-co": 0x240A, "es-es": 0x0C0A, "es-mx": 0x080A, "es-us": 0x540A, "et": 0x0425,
	"eu": 0x042D, "fa": 0x0429, "fi": 0x040B, "fil": 0x0464, "fo": 0x0438,
	"fr": 0x040C, "fr-be": 0x080C, "fr-ca": 0x0C0C, "fr-ch": 0x100C, "fr-fr": 0x040C,
	"ga": 0x083C, "gd": 0x0491, "gl": 0x0456, "gsw": 0x0484, "gu": 0x0447,
	"ha": 0x0468, "he": 0x040D, "hi": 0x0439, "hr": 0x041A, "hsb": 0x042E,
	"hu": 0x040E, "hy": 0x042B, "id": 0x0421, "ig": 0x0470, "ii": 0x0478,
	"is": 0x040F, "it": 0x0410, "it-ch": 0x0810, "it-it": 0x0410, "iu": 0x045D,
	"ja": 0x0411, "ka": 0x0437, "kk": 0x043F, "kl": 0x046F, "km": 0x0453,
	"kn": 0x044B, "ko": 0x0412, "kok": 0x0457, "ky": 0x0440, "lb": 0x046E,
	"lo": 0x0454, "lt": 0x0427, "lv": 0x0426, "mi": 0x0481, "mk": 0x042F,
	"ml": 0x044C, "mn": 0x0450, "moh": 0x047C, "mr": 0x044E, "ms": 0x043E,
	"mt": 0x043A, "nb": 0x0414, "ne": 0x0461, "nl": 0x0413, "nl-be": 0x0813,
	"nl-nl": 0x0413, "nn": 0x0814, "no": 0x0414, "nso": 0x046C, "oc": 0x0482,
	"or": 0x0448, "pa": 0x0446, "pl": 0x0415, "prs": 0x048C, "ps": 0x0463,
	"pt": 0x0816, "pt-br": 0x0416, "pt-pt": 0x0816, "qut": 0x0486, "quz": 0x046B,
	"rm": 0x0417, "ro": 0x0418, "ru": 0x0419, "rw": 0x0487, "sa": 0x044F,
	"sah": 0x0485, "se": 0x043B, "si": 0x045B, "sk": 0x041B, "sl": 0x0424,
	"sq": 0x041C, "sr": 0x281A, "sr-cyrl": 0x281A, "sr-latn": 0x241A, "sv": 0x041D,
	"sw": 0x0441, "ta": 0x0449, "te": 0x044A, "tg": 0x0428, "th": 0x041E,
	"tn": 0x0432, "tr": 0x041F, "tt": 0x0444, "ug": 0x0480, "uk": 0x0422,
	"ur": 0x0420, "uz": 0x0443, "vi": 0x042A, "wo": 0x0488, "xh": 0x0434,
	"yo": 0x046A, "zh": 0x0804, "zh-cn": 0x0804, "zh-hans": 0x0804, "zh-hant": 0x0404,
	"zh-hant-hk": 0x0C04, "zh-hant-mo": 0x1404, "zh-hk": 0x0C04, "zh-mo": 0x1404,
	"zh-sg": 0x1004, "zh-tw": 0x0404, "zu": 0x0435,
}

// nsisLanguages are the languages NSIS names by its language files, which
// MUI_LANGUAGE takes without the .nlf extension: the languages and scripts
// of the tags of a system, with the language alone where NSIS has one.
var nsisLanguages = map[string]string{
	"af": "Afrikaans", "ar": "Arabic", "az": "Azerbaijani", "be": "Belarusian",
	"bg": "Bulgarian", "bn": "Bengali", "bs": "Bosnian", "ca": "Catalan",
	"cs": "Czech", "cy": "Welsh", "da": "Danish", "de": "German",
	"el": "Greek", "en": "English", "es": "Spanish", "es-419": "SpanishInternational",
	"et": "Estonian", "eu": "Basque", "fa": "Farsi", "fi": "Finnish",
	"fr": "French", "ga": "Irish", "gl": "Galician", "he": "Hebrew",
	"hi": "Hindi", "hr": "Croatian", "hu": "Hungarian", "hy": "Armenian",
	"id": "Indonesian", "is": "Icelandic", "it": "Italian", "ja": "Japanese",
	"ka": "Georgian", "kk": "Kazakh", "ko": "Korean", "lb": "Luxembourgish",
	"lt": "Lithuanian", "lv": "Latvian", "mk": "Macedonian", "mn": "Mongolian",
	"ms": "Malay", "nb": "Norwegian", "nl": "Dutch", "nn": "NorwegianNynorsk",
	"no": "Norwegian", "pl": "Polish", "pt": "Portuguese", "pt-br": "PortugueseBR",
	"ro": "Romanian", "ru": "Russian", "sk": "Slovak", "sl": "Slovenian",
	"sq": "Albanian", "sr": "Serbian", "sr-latn": "SerbianLatin", "sv": "Swedish",
	"sw": "Swahili", "th": "Thai", "tr": "Turkish", "uk": "Ukrainian",
	"ur": "Urdu", "uz": "Uzbek", "vi": "Vietnamese", "zh": "SimpChinese",
	"zh-hans": "SimpChinese", "zh-hant": "TradChinese",
}
