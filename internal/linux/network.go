//go:build linux && (amd64 || arm64)

package linux

import (
	"github.com/egoist/mygo/internal/platform"
)

// GNetworkMonitor is a singleton GLib keeps up to date, so a status is a
// plain read and the network-changed signal drives the event.
type network struct{ b *Backend }

func (b *Backend) Network() platform.Network { return network{b} }

// Status reads the monitor's current view. It runs on the main thread,
// which is where g_network_monitor_get_default initializes the singleton.
func (network) Status() platform.NetworkStatus {
	m := gNetworkMonitorGetDefault()
	if m == 0 {
		return platform.NetworkStatus{}
	}
	return platform.NetworkStatus{
		Online:      gNetworkMonitorGetNetworkAvailable(m),
		Constrained: gNetworkMonitorGetNetworkMetered(m),
	}
}

// startNetworkMonitor connects network-changed once, from Backend.Init on
// the main thread, so that Status answers from the very first call.
func startNetworkMonitor() {
	if m := gNetworkMonitorGetDefault(); m != 0 {
		connect(m, "network-changed", cbNetworkChanged, 0)
	}
}
