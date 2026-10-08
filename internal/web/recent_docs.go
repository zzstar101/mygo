//go:build js

package web

import "github.com/egoist/mygo/internal/platform"

func (app) AddRecentDocument(string) error            { return errUnsupported }
func (app) ClearRecentDocuments() error               { return errUnsupported }
func (app) SetJumpList([]platform.JumpListTask) error { return errUnsupported }
