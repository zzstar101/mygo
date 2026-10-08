//go:build windows && (amd64 || arm64)

package windows

import (
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
)

var iphlpapi = systemDLL("iphlpapi.dll")

var (
	procNotifyIpInterfaceChange    = iphlpapi.NewProc("NotifyIpInterfaceChange")
	procGetNetworkConnectivityHint = iphlpapi.NewProc("GetNetworkConnectivityHint")
)

const (
	afUnspec = 0

	nlConnectivityLevelInternetAccess      = 3
	nlConnectivityLevelConstrainedInternet = 4

	nlConnectivityCostVariable = 3
)

// nlNetworkConnectivityHint mirrors NL_NETWORK_CONNECTIVITY_HINT of nldef.h:
// the connectivity level and cost are C enums (int, 4 bytes each), followed
// by three BOOLEANs. Only the level and the cost are read here.
type nlNetworkConnectivityHint struct {
	Level            int32
	Cost             int32
	ApproachingLimit uint8
	OverLimit        uint8
	Roaming          uint8
}

func (b *Backend) Network() platform.Network { return network{b} }

type network struct{ b *Backend }

// Status reads the system's connectivity hint.
func (network) Status() platform.NetworkStatus {
	if !has(procGetNetworkConnectivityHint) {
		// Windows 10 before 2004 has no hint. Assume the connection works
		// and is not metered, as the browsers do.
		return platform.NetworkStatus{Online: true}
	}
	var h nlNetworkConnectivityHint
	if r, _, _ := procGetNetworkConnectivityHint.Call(uintptr(unsafe.Pointer(&h))); r != 0 {
		return platform.NetworkStatus{Online: true}
	}
	return platform.NetworkStatus{
		Online:      h.Level == nlConnectivityLevelInternetAccess || h.Level == nlConnectivityLevelConstrainedInternet,
		Constrained: h.Cost == nlConnectivityCostVariable,
	}
}

// networkCallback posts a message to the app window when the set of
// interfaces changes. NotifyIpInterfaceChange calls it on a system thread, so
// it must not touch the backend: the main thread reads the status in
// appMessage. startNetworkMonitor creates it once.
var networkCallback uintptr

// networkHandle is the NotifyIpInterfaceChange registration. Windows removes
// it when the process exits, so it is never cancelled (and CancelMibChangeNotify2
// must not be called from the callback).
var networkHandle uintptr

// startNetworkMonitor asks to be told about interface changes, once, from
// Backend.Init on the main thread.
func startNetworkMonitor() {
	if networkCallback != 0 {
		return
	}
	// syscall.NewCallback needs one uintptr-sized result and uintptr-sized
	// arguments.
	networkCallback = syscall.NewCallback(func(ctx, row, notify uintptr) uintptr {
		if theBackend.appHwnd != 0 {
			postMessage(theBackend.appHwnd, wmAppNetwork, 0, 0)
		}
		return 0
	})
	procNotifyIpInterfaceChange.Call(afUnspec, networkCallback, 0, 1, uintptr(unsafe.Pointer(&networkHandle)))
}
