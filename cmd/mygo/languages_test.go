package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// utf16Of encodes a string the way a Windows resource holds it, so that a
// resource can be searched for the strings it holds.
func utf16Of(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func TestLanguageTagForms(t *testing.T) {
	for _, tc := range []struct{ tag, desktop, base, script string }{
		{"en", "en", "en", "en"},
		{"en-GB", "en_GB", "en", "en"},
		{"zh-Hans", "zh_Hans", "zh", "zh-hans"},
		{"zh-Hant-HK", "zh_Hant_HK", "zh", "zh-hant"},
		{"zh", "zh", "zh", "zh-hans"},
		{"zh-CN", "zh_CN", "zh", "zh-hans"},
		{"zh-SG", "zh_SG", "zh", "zh-hans"},
		{"zh-TW", "zh_TW", "zh", "zh-hant"},
		{"zh-HK", "zh_HK", "zh", "zh-hant"},
		{"zh-MO", "zh_MO", "zh", "zh-hant"},
		{"pt-BR", "pt_BR", "pt", "pt"},
		{"es-419", "es_419", "es", "es"},
	} {
		if got := desktopTag(tc.tag); got != tc.desktop {
			t.Errorf("desktopTag(%q) = %q, want %q", tc.tag, got, tc.desktop)
		}
		if got := baseLanguage(tc.tag); got != tc.base {
			t.Errorf("baseLanguage(%q) = %q, want %q", tc.tag, got, tc.base)
		}
		if got := scriptLanguage(tc.tag); got != tc.script {
			t.Errorf("scriptLanguage(%q) = %q, want %q", tc.tag, got, tc.script)
		}
	}
}

func TestWindowsLanguageID(t *testing.T) {
	for _, tc := range []struct {
		tag string
		id  uint16
		ok  bool
	}{
		{"en", langEnUS, true},
		{"en-US", langEnUS, true},
		{"en-GB", 0x0809, true},
		{"zh", 0x0804, true},
		{"zh-Hans", 0x0804, true},
		{"zh-Hans-CN", 0x0804, true},
		{"zh-Hant", 0x0404, true},
		{"zh-Hant-HK", 0x0C04, true},
		{"pt", 0x0816, true},
		{"pt-BR", 0x0416, true},
		{"pt-PT", 0x0816, true},
		{"tlh", 0, false}, // Klingon, which Windows has no language for
	} {
		id, ok := windowsLanguageID(tc.tag)
		if id != tc.id || ok != tc.ok {
			t.Errorf("windowsLanguageID(%q) = %#04x, %v, want %#04x, %v", tc.tag, id, ok, tc.id, tc.ok)
		}
	}
}

func TestNSISLanguage(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		name string
		ok   bool
	}{
		{"en", "English", true},
		{"en-GB", "English", true},
		{"zh", "SimpChinese", true},
		{"zh-Hans", "SimpChinese", true},
		{"zh-CN", "SimpChinese", true},
		{"zh-Hant", "TradChinese", true},
		{"zh-Hant-HK", "TradChinese", true},
		{"zh-TW", "TradChinese", true},
		{"zh-HK", "TradChinese", true},
		{"zh-MO", "TradChinese", true},
		{"zh-SG", "SimpChinese", true},
		{"pt-BR", "PortugueseBR", true},
		{"pt", "Portuguese", true},
		{"fr", "French", true},
		{"tlh", "", false},
	} {
		name, ok := nsisLanguage(tc.tag)
		if name != tc.name || ok != tc.ok {
			t.Errorf("nsisLanguage(%q) = %q, %v, want %q, %v", tc.tag, name, ok, tc.name, tc.ok)
		}
	}
}

// TestNSISLanguageMacros checks the languages the Windows installer offers,
// whose first is the one it is in where the system language is not among
// them. NSIS knows one language file per language at most, so languages that
// share one appear once.
func TestNSISLanguageMacros(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "makensis")
	if got := nsisLanguageMacros(&Config{}, tool); got != "!insertmacro MUI_LANGUAGE \"English\"\n" {
		t.Errorf("an app without languages: %q", got)
	}
	c := &Config{Languages: []Language{{Tag: "zh-Hans"}, {Tag: "zh-CN"}, {Tag: "zh-Hant"}, {Tag: "fr"}, {Tag: "tlh"}}}
	want := `!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "TradChinese"
!insertmacro MUI_LANGUAGE "French"
`
	if got := nsisLanguageMacros(c, tool); got != want {
		t.Errorf("the installer languages are\n%swant\n%s", got, want)
	}
}

// TestLanguageConfig checks the configuration of the languages of an app:
// what a language leaves out comes from the app itself.
func TestLanguageConfig(t *testing.T) {
	dir := t.TempDir()
	config := `{
  "name": "Notes",
  "linux": {"comment": "Take notes"},
  "languages": [
    {"tag": "zh-Hans", "name": "笔记", "comment": "记笔记", "keywords": ["笔记", "记事本"]},
    {"tag": "fr"}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, jsonConfig), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Languages) != 2 {
		t.Fatalf("the config has %d languages", len(c.Languages))
	}
	if got := c.Languages[0]; got.Name != "笔记" || got.Comment != "记笔记" || len(got.Keywords) != 2 {
		t.Errorf("the first language is %+v", got)
	}
	if got := c.Languages[1]; got.Name != "Notes" || got.Comment != "Take notes" {
		t.Errorf("the second language is %+v", got)
	}

	for _, tc := range []struct {
		what  string
		langs []Language
		want  string
	}{
		{"a tag that is not one", []Language{{Tag: "chinese"}}, "not a language tag"},
		{"the same language twice", []Language{{Tag: "en"}, {Tag: "EN"}}, "more than once"},
		{"a name of two lines", []Language{{Tag: "en", Name: "Notes\nand more"}}, "cannot break the line"},
		{"a keyword of two", []Language{{Tag: "en", Keywords: []string{"note;book"}}}, "not a keyword"},
	} {
		err := (&Config{Languages: tc.langs}).validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.what, err, tc.want)
		}
	}
	if err := (&Config{Languages: []Language{{Tag: "en"}, {Tag: "zh-Hant-HK"}}}).validate(); err != nil {
		t.Errorf("valid languages: %v", err)
	}
}

// TestLanguagePackaging checks what a build tells each system about the
// languages of the app.
func TestLanguagePackaging(t *testing.T) {
	c := &Config{
		Name:       "Notes",
		Identifier: "com.example.notes",
		Version:    "1.2.3",
		Linux:      Linux{Comment: "Take notes"},
		Languages: []Language{
			{Tag: "zh-Hans", Name: "笔记", Comment: "记笔记", Keywords: []string{"笔记", "记事本"}},
			{Tag: "fr", Name: "Notes FR", Keywords: []string{"notes", "carnet"}},
		},
	}

	// macOS: the development region, which the system falls back to, the
	// localizations, and the app's name in each language.
	plist := string(infoPlist(c, "Notes", "", ""))
	for _, want := range []string{
		"<key>CFBundleDevelopmentRegion</key>",
		"<string>zh-Hans</string>",
		"<key>CFBundleLocalizations</key>",
		"<string>fr</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("the Info.plist has no %s:\n%s", want, plist)
		}
	}
	res := t.TempDir()
	if err := writeLocalizations(c, res); err != nil {
		t.Fatal(err)
	}
	stringsFile, err := os.ReadFile(filepath.Join(res, "zh-Hans.lproj", "InfoPlist.strings"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stringsFile), "<string>笔记</string>") {
		t.Errorf("the localization of zh-Hans is\n%s", stringsFile)
	}
	if _, err := os.Stat(filepath.Join(res, "fr.lproj", "InfoPlist.strings")); err != nil {
		t.Errorf("no localization of fr: %v", err)
	}
	plain := t.TempDir()
	if err := writeLocalizations(&Config{Name: "Notes", Languages: []Language{{Tag: "en", Name: "Notes"}}}, plain); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plain, "en.lproj")); !os.IsNotExist(err) {
		t.Errorf("a language that names the app the same needs no file: %v", err)
	}

	// Linux: the name, the comment and the keywords of each language, after
	// the ones of the app's own language, which a launcher shows where the
	// system language is not among them.
	entry := linuxDesktopEntry(c, "notes", "notes")
	for _, want := range []string{
		"Type=Application\n",
		"Name=笔记\n",
		"Name[fr]=Notes FR\n",
		"Comment=记笔记\n",
		"Keywords=笔记;记事本;\n",
		"Keywords[fr]=notes;carnet;\n",
	} {
		if !strings.Contains(entry, want) {
			t.Errorf("the desktop entry has no %q:\n%s", want, entry)
		}
	}
	for _, unwanted := range []string{"Name[zh_Hans]", "Comment[zh_Hans]", "Comment[fr]", "Keywords[zh_Hans]", "Name[fr]=Notes\n"} {
		if strings.Contains(entry, unwanted) {
			t.Errorf("the desktop entry has %q:\n%s", unwanted, entry)
		}
	}

	// Windows: one string table per language, and the languages listed.
	langs := c.windowsLanguages()
	if len(langs) != 2 || langs[0] != (windowsLanguage{id: 0x0804, name: "笔记"}) || langs[1] != (windowsLanguage{id: 0x040C, name: "Notes FR"}) {
		t.Fatalf("the Windows languages are %+v", langs)
	}
	ver := versionInfo(c, langs)
	for _, want := range []string{"StringFileInfo", "080404b0", "040c04b0", "笔记", "Notes FR", "Translation"} {
		if !bytes.Contains(ver, utf16Of(want)) {
			t.Errorf("the version resource has no %q:\n% x", want, ver)
		}
	}
	if want := []byte{0x04, 0x08, 0xB0, 0x04, 0x0C, 0x04, 0xB0, 0x04}; !bytes.Contains(ver, want) {
		t.Errorf("the version resource does not list zh-CN and fr-FR, Unicode: % x", ver)
	}
	syso, err := winresSyso(c, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(syso, utf16Of("080404b0")) {
		t.Error("the Windows resources have no version table of zh-Hans")
	}
	if !bytes.Contains(syso, []byte{0x04, 0x08, 0x00, 0x00}) {
		t.Error("the Windows resources are not in the language of the app")
	}
}
