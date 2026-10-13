//go:build !js

package mygo

import (
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestNetwork(t *testing.T) {
	set := func(online, constrained bool) {
		onMain(func() { fb.Online, fb.Constrained = online, constrained })
	}
	set(true, false)
	defer set(false, false)
	if s := Network.Status(); s != (NetworkStatus{Online: true}) {
		t.Errorf("Status = %+v, want online", s)
	}
	set(true, true)
	if s := Network.Status(); s != (NetworkStatus{Online: true, Constrained: true}) {
		t.Errorf("Status = %+v, want online and constrained", s)
	}

	var got []NetworkStatus
	mainOK := true
	off := Network.OnChanged(func(s NetworkStatus) {
		got = append(got, s)
		mainOK = mainOK && isMainThread()
	})
	defer off()
	deadline := time.Now().Add(time.Second)
	for !onMainValue(func() bool { return fb.WatchingNetwork }) {
		if time.Now().After(deadline) {
			t.Fatal("the backend was not asked to watch the network")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The status when watching started is the one changes are told from,
	// and a report that changes nothing calls no listener.
	onMain(func() {
		fb.EmitNetwork(platform.NetworkStatus{Online: true, Constrained: true})
		fb.EmitNetwork(platform.NetworkStatus{Online: false})
		fb.EmitNetwork(platform.NetworkStatus{Online: false})
		fb.EmitNetwork(platform.NetworkStatus{Online: true})
	})
	want := []NetworkStatus{{}, {Online: true}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
	if !mainOK {
		t.Error("OnChanged did not run on the main thread")
	}

	off()
	onMain(func() { fb.EmitNetwork(platform.NetworkStatus{Constrained: true}) })
	if len(got) != len(want) {
		t.Errorf("changes after off = %+v, want %+v", got, want)
	}
}
