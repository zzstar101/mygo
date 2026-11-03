//go:build !mygo_noinspector

package surface

// ConnReload is what a restart of `mygo dev` needs of the content.
type ConnReload struct {
	// Reload tells that mygo dev launched the window with state
	// handover: the content then names its elements so that the next
	// build finds their state again.
	Reload bool
	// ReloadSave returns what the content keeps across a reload of
	// `mygo dev`, and ReloadRestore gives it back before the window's
	// first frame. The framework calls them on the main thread, and they
	// are nil for a content that keeps nothing (package ui sets them).
	ReloadSave    func() []byte
	ReloadRestore func([]byte)
}
