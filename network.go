package mygo

import (
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// NetworkModule reports whether the computer is online. Use the Network
// singleton.
type NetworkModule struct {
	mu       sync.Mutex
	watching bool
	// last is the status listeners last saw, so they are only called when
	// something changed: the systems report changes of routes and
	// interfaces that leave the status as it was.
	last NetworkStatus

	onChanged listeners[func(NetworkStatus)]
}

// Network reports whether the computer is online.
var Network = &NetworkModule{}

// NetworkStatus is the state of the computer's network connection.
type NetworkStatus struct {
	// Online reports whether the system has a usable connection, one that
	// leads beyond the computer. It does not prove that a server answers.
	Online bool
	// Constrained reports whether the connection is expensive or metered,
	// so the app should send less, or ask the user before it downloads
	// something large. It is false where the system does not say.
	Constrained bool
}

// Status returns the computer's network state.
func (n *NetworkModule) Status() NetworkStatus {
	needsApp("Network.Status")
	return onMainValue(func() NetworkStatus { return networkStatus(backend().Network().Status()) })
}

// OnChanged is called on the main thread when the computer's network state
// changes, with the new state.
func (n *NetworkModule) OnChanged(fn func(status NetworkStatus)) (off func()) {
	off = n.onChanged.add(fn, false)
	n.mu.Lock()
	start := !n.watching
	n.watching = true
	n.mu.Unlock()
	if start {
		App.WhenReady(func() {
			b := backend().Network()
			b.Watch()
			s := networkStatus(b.Status())
			n.mu.Lock()
			n.last = s
			n.mu.Unlock()
		})
	}
	return off
}

// changed records a status from the backend and calls the listeners when
// it differs from the last one.
func (n *NetworkModule) changed(status NetworkStatus) {
	n.mu.Lock()
	same := status == n.last
	n.last = status
	n.mu.Unlock()
	if !same {
		fire1(&n.onChanged, status)
	}
}

func networkStatus(s platform.NetworkStatus) NetworkStatus {
	return NetworkStatus{Online: s.Online, Constrained: s.Constrained}
}

var _ platform.Network // the backends implement it
