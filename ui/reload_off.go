//go:build mygo_noinspector

package ui

import "github.com/egoist/mygo/internal/surface"

// Production builds (mygo build, as for the inspector) are never run by
// mygo dev: what package ui keeps across its reloads is left out, so that
// it takes no room in the binary, and no field in an element.
const reloadable = false

type (
	nodeReload   struct{}
	stateReload  struct{}
	engineReload struct{}
)

func (h *windowHost) attachReload(*surface.Conn)        {}
func (e *node) reloadNameRoot()                         {}
func (rt *engine) reloadNamePlace(_, _ *node, _ uint64) {}
func (rt *engine) reloadNameKey(_, _ *node, _ any)      {}
func (rt *engine) reloadBorn(*node)                     {}
func (rt *engine) reloadRouter(*Router)                 {}
func (s *state) reloadTakeScroll(uint64)                {}
func (s *state) reloadTakeCaret(*editor)                {}
func (r *Route) namePage(int)                           {}
