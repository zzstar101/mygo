//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/mygo/internal/platform"
)

// Network.framework, a C API: no Objective-C objects, no reference
// counting beyond the monitor itself, and blocks instead of a delegate.
var (
	libNetwork uintptr

	nwPathMonitorCreate           func() uintptr
	nwPathMonitorSetUpdateHandler func(monitor, handler uintptr)
	nwPathMonitorSetQueue         func(monitor, queue uintptr)
	nwPathMonitorStart            func(monitor uintptr)
	nwPathGetStatus               func(path uintptr) int32
	nwPathIsExpensive             func(path uintptr) bool
	nwPathIsConstrained           func(path uintptr) bool

	// dispatchGetMainQueue is the address of _dispatch_main_q, the main
	// queue. dispatch_get_main_queue itself is a static inline in the SDK
	// that returns it, so it has no symbol to bind.
	dispatchGetMainQueue uintptr
)

// nw_path_status_satisfied: the path has a usable route to send and
// receive data on.
const nwPathStatusSatisfied = 1

// networkMonitor is the app's single path monitor and the status it last
// reported. Both are only touched on the main thread, which is where the
// monitor's queue dispatches its blocks.
var networkMonitor struct {
	monitor uintptr
	status  platform.NetworkStatus
}

func loadNetwork() {
	libNetwork = mustDlopen("/System/Library/Frameworks/Network.framework/Network")
	purego.RegisterLibFunc(&nwPathMonitorCreate, libNetwork, "nw_path_monitor_create")
	purego.RegisterLibFunc(&nwPathMonitorSetUpdateHandler, libNetwork, "nw_path_monitor_set_update_handler")
	purego.RegisterLibFunc(&nwPathMonitorSetQueue, libNetwork, "nw_path_monitor_set_queue")
	purego.RegisterLibFunc(&nwPathMonitorStart, libNetwork, "nw_path_monitor_start")
	purego.RegisterLibFunc(&nwPathGetStatus, libNetwork, "nw_path_get_status")
	purego.RegisterLibFunc(&nwPathIsExpensive, libNetwork, "nw_path_is_expensive")
	purego.RegisterLibFunc(&nwPathIsConstrained, libNetwork, "nw_path_is_constrained")
	dispatchGetMainQueue = mustDlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
}

type network struct{ b *Backend }

func (b *Backend) Network() platform.Network { return network{b} }

// Status returns the status the monitor last reported.
func (network) Status() platform.NetworkStatus { return networkMonitor.status }

// startNetworkMonitor starts the app's single path monitor. It is called
// once, from Backend.Init, on the main thread, so that Status answers from
// the very first call.
func startNetworkMonitor() {
	m := nwPathMonitorCreate()
	if m == 0 {
		// Only a failed allocation gets here. Reporting offline is the
		// safe answer for an app that syncs or downloads.
		return
	}
	// The block is scheduled on the main queue, which is MyGo's main
	// thread, so it may call the handler directly. It touches no
	// Objective-C object, so it needs no autorelease pool.
	blk := newBlock(func(_ objc.Block, path uintptr) {
		status := platform.NetworkStatus{
			Online:      nwPathGetStatus(path) == nwPathStatusSatisfied,
			Constrained: nwPathIsExpensive(path) || nwPathIsConstrained(path),
		}
		networkMonitor.status = status
		theBackend.h.NetworkChanged(status)
	})
	// The framework keeps the handler, so hand it a copy the block can
	// outlive its stack frame with, as the window handlers do.
	nwPathMonitorSetUpdateHandler(m, uintptr(blk.Copy()))
	nwPathMonitorSetQueue(m, dispatchGetMainQueue)
	nwPathMonitorStart(m)
	// The monitor is retained by the framework and lives for the process.
	// There is no teardown hook that would run early enough to cancel it
	// safely, and cancelling would need a cancel handler too.
	networkMonitor.monitor = m
}
