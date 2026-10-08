package mygo

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// NetworkModule reports whether the computer is online. Use the Network
// singleton.
type NetworkModule struct {
	mu sync.Mutex
	// last is the status the system last reported, so listeners are only
	// called when something actually changed.
	last NetworkStatus

	onChanged listeners[func(NetworkStatus)]
}

// Network reports whether the computer is online.
var Network = &NetworkModule{}

// NetworkStatus is the state of the computer's network connection.
type NetworkStatus struct {
	// Online reports whether the computer can reach the internet.
	Online bool
	// Constrained reports whether the connection is expensive or metered,
	// so the app should send less, or ask the user before it downloads
	// something large. It is false where the system does not say.
	Constrained bool
}

// OnChanged is called when the computer's network state changes, with the
// new state. It is safe to call from any goroutine; the call runs on the
// main thread.
func (n *NetworkModule) OnChanged(fn func(status NetworkStatus)) (off func()) {
	return n.onChanged.add(fn, false)
}

// Status returns the computer's network state. It reports what the system
// last told MyGo, which the backends keep up to date from the moment the
// application starts, so it answers correctly on the first call.
func (n *NetworkModule) Status() NetworkStatus {
	needsApp("Network.Status")
	return onMainValue(func() NetworkStatus {
		return networkStatus(backend().Network().Status())
	})
}

// changed records a status from a backend and calls the listeners when it
// differs from the last one.
func (n *NetworkModule) changed(status NetworkStatus) {
	n.mu.Lock()
	if status == n.last {
		n.mu.Unlock()
		return
	}
	n.last = status
	n.mu.Unlock()
	fire1(&n.onChanged, status)
}

func networkStatus(s platform.NetworkStatus) NetworkStatus {
	return NetworkStatus{Online: s.Online, Constrained: s.Constrained}
}

var _ platform.Network // the backends implement it
