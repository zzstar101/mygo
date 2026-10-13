package windows

import (
	"syscall"
	"testing"
	"unsafe"
)

// TestLocaleIsDisplayLanguage holds Locale to the display language of the
// user, the first one GetUserDefaultUILanguage names too, and not to the
// language of the regional formats.
func TestLocaleIsDisplayLanguage(t *testing.T) {
	got := appController{}.Locale()
	if got == "" {
		t.Fatal("no locale")
	}
	id, _, _ := procGetUserDefaultUILanguage.Call()
	buf := make([]uint16, 85)
	n, _, _ := procLCIDToLocaleName.Call(id, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 {
		t.Skip("Windows names no display language")
	}
	// The preferred display languages may name a language the default
	// one is a region of, as en-GB for en-US: they share the language.
	ui := syscall.UTF16ToString(buf)
	if base(got) != base(ui) {
		t.Errorf("Locale() = %q, the display language is %q", got, ui)
	}
}

func base(tag string) string {
	for i, r := range tag {
		if r == '-' {
			return tag[:i]
		}
	}
	return tag
}
