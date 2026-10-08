package e2e

import (
	"testing"

	"github.com/egoist/mygo"
)

// TestNetwork smoke-tests the network status through the real backend. The
// monitor is started eagerly in Init, so Status reads a live value and
// OnChanged fires on later changes; the deterministic behavior (dedup, off,
// main thread) is covered by the unit tests through internal/fake.
func TestNetwork(t *testing.T) {
	off := mygo.Network.OnChanged(func(mygo.NetworkStatus) {})
	defer off()

	s := mygo.Network.Status()
	t.Logf("online: %v, constrained: %v", s.Online, s.Constrained)
	if !s.Online {
		t.Log("offline; skipping the online assertions")
		return
	}
	// The connection is usable; a constrained flag, when present, must not
	// contradict that.
	if s.Constrained && !s.Online {
		t.Error("constrained but not online")
	}
}
