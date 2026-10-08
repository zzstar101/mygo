// Package callcontext carries private page-lifetime metadata to official plugins.
package callcontext

// PageKey identifies the original page's Done channel, shared by its IPC calls.
// Unlike a call context, it changes only on navigation or window closure.
type PageKey struct{}
