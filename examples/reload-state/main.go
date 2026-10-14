// Reload-state is a note that outlives the rebuild of `mygo dev`: the
// process starts again, and what you typed, the counter and the window come
// back with it.
//
//	go run ./cmd/mygo dev examples/reload-state
package main

import (
	"fmt"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// note is the state the window shows, which the app hands over on a reload.
type note struct {
	draft string
	lines []string
	count int
}

func (s *note) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(16).Gap(10).Children(func() {
		ui.Text(c, "Type, scroll, then edit this file").Bold()
		ui.Textf(c, "%d lines kept · counted %d", len(s.lines), s.count).FontSize(12).TextColor(t.TextMuted)
		input := ui.TextInput(c.Key("draft"), &s.draft).Placeholder("A line to keep").AutoFocus()
		ui.Row(c).Gap(8).Children(func() {
			if input.Submitted() || ui.Button(c.Key("add"), "Add").Clicked() {
				if s.draft != "" {
					s.lines = append([]string{s.draft}, s.lines...)
					s.draft = ""
				}
			}
			if ui.Button(c.Key("count"), fmt.Sprintf("Count %d", s.count)).Clicked() {
				s.count++
			}
			if ui.Button(c.Key("clear"), "Clear").Disabled(len(s.lines) == 0).Clicked() {
				s.lines = nil
			}
		})
		ui.Scroll(c).Grow(1).Gap(6).Children(func() {
			if len(s.lines) == 0 {
				ui.Text(c, "Nothing kept yet").TextColor(t.TextMuted)
			}
			for i, line := range s.lines {
				ui.Textf(c, "%d. %s", i+1, line)
			}
		})
	})
}

func main() {
	s := &note{}
	// What survives the reload. Without it the app starts fresh: the window
	// still comes back where it was, with the state package ui keeps of its
	// own elements, but nothing the app itself holds does.
	mygo.App.OnReload(mygo.ReloadHandlers{
		Save: func(r *mygo.ReloadState) error {
			if err := r.Set("draft", s.draft); err != nil {
				return err
			}
			if err := r.Set("lines", s.lines); err != nil {
				return err
			}
			return r.Set("count", s.count)
		},
		Restore: func(r *mygo.ReloadState) error {
			// A key the build before this one never set is not an error.
			if _, err := r.Get("draft", &s.draft); err != nil {
				return err
			}
			if _, err := r.Get("lines", &s.lines); err != nil {
				return err
			}
			_, err := r.Get("count", &s.count)
			return err
		},
	})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Reload",
			Width:   420,
			Height:  380,
			Content: ui.View(s.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
