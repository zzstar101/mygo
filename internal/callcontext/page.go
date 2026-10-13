// Package callcontext holds the keys of what package mygo puts in the
// context of calls for the official plugins.
package callcontext

// PageKey is the key of the Done channel of the calling page's context,
// which navigating or closing the window replaces, unlike the page.
type PageKey struct{}
