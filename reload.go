package mygo

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ReloadHandlers are what an app hands from one build of `mygo dev -hot` to
// the next as it restarts, for App.OnReload.
//
// Save runs on the main thread before the running build quits, in the dev
// output when it fails: put what should survive the reload into s. What
// Save sets is the whole state the app hands over, so a key a build no
// longer sets does not outlive the code that set it. The build that starts
// next reads it in Restore, which runs on the main thread before the app's
// WhenReady callbacks create its windows.
//
// Either may be nil, and what MyGo keeps itself (the window states of the
// reload, and what package ui keeps of its elements) is handed over
// whether or not they are set.
type ReloadHandlers struct {
	Save    func(s *ReloadState) error
	Restore func(s *ReloadState) error
}

// ReloadState is what a reload hands over: the app's values, by set key,
// next to what MyGo keeps by itself.
type ReloadState struct {
	values map[string]json.RawMessage
}

// Set stores value under key. The value must encode to JSON; a value that
// does not is only reported in the dev output, as the reload goes on.
func (s *ReloadState) Set(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.values == nil {
		s.values = map[string]json.RawMessage{}
	}
	s.values[key] = data
	return nil
}

// Get reads the value of key into value, which must be a pointer. It reports
// whether the key was set, and why it could not be read when it was: a key
// the build before this one never set is not an error, a value of a shape
// this build does not know is one.
func (s *ReloadState) Get(key string, value any) (bool, error) {
	data, ok := s.values[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(data, value); err != nil {
		return false, fmt.Errorf("mygo: reload: %s: %w", key, err)
	}
	return true, nil
}

// Keys lists the keys set so far, sorted.
func (s *ReloadState) Keys() []string {
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// OnReload registers what an app keeps across a restart of `mygo dev -hot`: the
// window is replaced and the process starts again, and Save hands what
// should survive to Restore, which runs before the first window of the
// new build.
//
//	mygo.App.OnReload(mygo.ReloadHandlers{
//		Save:    func(s *mygo.ReloadState) error { return s.Set("draft", app.draft) },
//		Restore: func(s *mygo.ReloadState) error { _, err := s.Get("draft", &app.draft); return err },
//	})
//
// Nothing else changes: MyGo keeps the window's bounds and the state of
// native UI elements itself, and the window is shown once it draws the
// restored state, so a reload does not flash an empty window.
//
// Outside `mygo dev -hot` the handlers are never called: nothing is listening
// for a reload, so the app neither saves nor restores anything. A build of
// `mygo build` is marked production, which keeps it from even dialing a
// dev channel, and the app it ships never hands its state to anything.
func (a *Application) OnReload(h ReloadHandlers) { setReloadHandlers(h) }
