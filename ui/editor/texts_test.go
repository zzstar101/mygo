package editor

import (
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The editor says its own strings in the language of the app's locale.
func TestEditorTextsLocale(t *testing.T) {
	ct := newEditorTest(t, "", "|cat dog cat\ncat")
	ct.st.Find("cat")
	ct.tt.SetLocale("zh-Hans")
	if !ct.tt.HasText("查找") || !ct.tt.HasText("第 1 项，共 3 项") {
		t.Fatalf("find bar not in Chinese: %q", ct.tt.Texts())
	}
	if ct.tt.HasText("Find") || ct.tt.HasText("Match whole word") {
		t.Fatalf("find bar still says English: %q", ct.tt.Texts())
	}
	// The replacement row says its own words too.
	if err := ct.tt.Click("替换"); err != nil {
		t.Fatal(err)
	}
	ct.tt.Frame()
	if !ct.tt.HasText("全部替换") {
		t.Fatalf("replace buttons not in Chinese: %q", ct.tt.Texts())
	}
	// The go to line bar, in both languages.
	ct.tt.SetLocale("")
	ct.run(GoToLine)
	if !ct.tt.HasText("Current line 1 of 2") {
		t.Fatalf("go to line bar not in English: %q", ct.tt.Texts())
	}
	ct.tt.SetLocale("zh-Hans")
	if !ct.tt.HasText("第 1 行，共 2 行") {
		t.Fatalf("go to line bar not in Chinese: %q", ct.tt.Texts())
	}
}

// An app changes the words the editor says of its own with ui.UseTexts.
func TestEditorTextsApp(t *testing.T) {
	ui.UseTexts(map[string]ui.Texts{
		"en": {Replace: "Swap", ReplaceAll: "Swap All"},
	})
	t.Cleanup(func() {
		ui.UseTexts(map[string]ui.Texts{
			"en": {Replace: "Replace", ReplaceAll: "Replace All"},
		})
	})
	ct := newEditorTest(t, "", "|cat dog cat")
	ct.st.Find("cat")
	ct.tt.Frame()
	if err := ct.tt.Click("Swap"); err != nil {
		t.Fatal(err)
	}
	ct.tt.Frame()
	if !ct.tt.HasText("Swap All") {
		t.Fatalf("the app's words not said: %q", ct.tt.Texts())
	}
	if ct.tt.HasText("Replace") {
		t.Fatalf("the app's words did not replace the framework's: %q", ct.tt.Texts())
	}
}

// The diff viewer says its own strings in the language of the app's locale.
func TestDiffViewerTextsLocale(t *testing.T) {
	var oldText, newText strings.Builder
	for i := range 30 {
		line := "line " + strconv.Itoa(i) + "\n"
		oldText.WriteString(line)
		if i == 15 {
			newText.WriteString("changed\n")
			continue
		}
		newText.WriteString(line)
	}
	d := &DiffState{Old: NewBuffer(oldText.String()), New: NewBuffer(newText.String()), Editable: true}
	tt := ui.NewTester(func(c *ui.Context) {
		DiffView(c, d).Fill()
	}, 900, 500)
	tt.Frame()
	if !tt.HasText("12 unmodified lines") {
		t.Fatalf("separators not in English: %q", tt.Texts())
	}
	tt.SetLocale("zh-Hans")
	if !tt.HasText("12 行未修改") {
		t.Fatalf("separators not in Chinese: %q", tt.Texts())
	}
}
