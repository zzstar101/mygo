//go:build !js

package e2e

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// localePage records the locale the bridge carries as the page starts, and
// the mygo:locale events its listeners hear.
const localePage = `<!doctype html><p>locale</p><script>
window.started = mygo.locale;
window.heard = [];
mygo.on("mygo:locale", (tag) => window.heard.push(tag));
</script>`

// TestLocaleAtDocumentStart checks, in a real web view, that a page loaded
// after App.SetLocale starts with the new locale, and that its listeners
// hear of a locale the page did not have only, not as it loads.
func TestLocaleAtDocumentStart(t *testing.T) {
	t.Cleanup(func() { mygo.App.SetLocale("") })
	w := newWindow(t, mygo.WindowOptions{Hidden: true})
	w.Page().LoadHTML(localePage, "app://localhost/")
	waitFor(t, w, `window.started === `+jsString(mygo.App.Locale()))

	mygo.App.SetLocale("ja-JP")
	waitFor(t, w, `mygo.locale === "ja-JP" && window.heard.join() === "ja-JP"`)

	w.Page().Reload()
	waitFor(t, w, `window.started === "ja-JP" && document.readyState === "complete"`)
	// The page is told the locale again as it loads, which it had.
	time.Sleep(300 * time.Millisecond)
	waitFor(t, w, `window.heard.length === 0`)

	mygo.App.SetLocale("zh-Hans")
	waitFor(t, w, `window.heard.join() === "zh-Hans"`)
}

func jsString(s string) string {
	b := []byte{'"'}
	for _, r := range s {
		if r == '"' || r == '\\' {
			b = append(b, '\\')
		}
		b = append(b, string(r)...)
	}
	return string(append(b, '"'))
}
