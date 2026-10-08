//go:build !js

package mygo

import (
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestNetwork(t *testing.T) {
	var got []NetworkStatus
	mainOK := true
	off := Network.OnChanged(func(s NetworkStatus) {
		got = append(got, s)
		if !isMainThread() {
			mainOK = false
		}
	})
	defer off()

	// The backends keep the status current from Init, so OnChanged fires on
	// a real change and swallows a duplicate (§2.3).
	onMain(func() {
		fb.Online = true
		fb.EmitNetwork(platform.NetworkStatus{Online: true}) // first report
		fb.EmitNetwork(platform.NetworkStatus{Online: true}) // duplicate: ignored
		fb.Online = false
		fb.EmitNetwork(platform.NetworkStatus{Online: false, Constrained: true})
	})
	want := []NetworkStatus{{Online: true}, {Online: false, Constrained: true}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
	if !mainOK {
		t.Error("OnChanged did not run on the main thread")
	}

	// Status reflects the backend's current state.
	fb.Online = true
	if s := Network.Status(); !s.Online || s.Constrained {
		t.Errorf("Status = %+v, want online", s)
	}
	fb.Constrained = true
	if s := Network.Status(); !s.Online || !s.Constrained {
		t.Errorf("Status = %+v, want online and constrained", s)
	}

	// off removes the listener: later emits do not reach it.
	off()
	onMain(func() {
		fb.Online = false
		fb.Constrained = false
		fb.EmitNetwork(platform.NetworkStatus{})
	})
	if len(got) != len(want) {
		t.Errorf("changes after off = %+v, want it unchanged", got)
	}
}
