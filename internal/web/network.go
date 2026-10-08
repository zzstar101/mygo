//go:build js

package web

import "github.com/egoist/mygo/internal/platform"

func (*Backend) Network() platform.Network { return network{} }

type network struct{}

func (network) Status() platform.NetworkStatus { return platform.NetworkStatus{} }
