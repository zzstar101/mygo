package ui

import (
	"testing"
	"time"
)

// timelineTester returns a tester whose frames happen at the time the test
// sets, from a clock it starts, as a Timeline measures its playhead by.
func timelineTester(view func(*context)) (*Tester, *time.Time) {
	now := time.Unix(2000, 0)
	h := &headless{w: 400, h: 300, scale: 1}
	tt := &Tester{h: h, rt: newRuntime(view, h)}
	tt.rt.clock = func() time.Time { return now }
	tt.settle()
	return tt, &now
}

// at moves the tester's clock on by d and draws the frame that follows.
func step(tt *Tester, now *time.Time, d time.Duration) {
	*now = now.Add(d)
	tt.Frame()
}

// resting reports that nothing is asking for another frame.
func resting(t *testing.T, tt *Tester) {
	t.Helper()
	if tt.rt.animating || tt.h.requested.Load() {
		t.Fatal("kept asking for frames")
	}
}

func TestTimelinePlaysTweensOneAfterTheOther(t *testing.T) {
	var x, y float32
	var dur time.Duration
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "intro", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		y = tl.To(Tween{From: -50, To: 50, Duration: 300 * time.Millisecond})
		dur = tl.Duration()
	})
	if dur != 700*time.Millisecond {
		t.Fatalf("Duration = %v, want 700ms", dur)
	}
	closeFloat(t, x, 0)
	closeFloat(t, y, -50)
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 50)
	closeFloat(t, y, -50)
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 100)
	closeFloat(t, y, -50) // the second starts where the first ends
	step(tt, now, 150*time.Millisecond)
	closeFloat(t, x, 100)
	closeFloat(t, y, 0)
}

func TestTimelinePlacesTweens(t *testing.T) {
	var after, overlap, with, abs float32
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "places", TimelineOptions{})
		after = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		// 250ms to 450ms: 150ms before the one before it ends.
		with = tl.To(Tween{From: 0, To: 100, Duration: 200 * time.Millisecond},
			OverlapPrevious(150*time.Millisecond))
		// 550ms to 750ms: 100ms after the one before it ends.
		overlap = tl.To(Tween{From: 0, To: 100, Duration: 200 * time.Millisecond},
			AfterPrevious(100*time.Millisecond))
		// 550ms to 950ms: where the one before it starts.
		abs = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond}, WithPrevious())
	})
	closeFloat(t, after, 0)
	closeFloat(t, overlap, 0)
	closeFloat(t, with, 0)
	closeFloat(t, abs, 0)
	step(tt, now, 250*time.Millisecond)
	closeFloat(t, after, 62.5)
	closeFloat(t, overlap, 0)
	closeFloat(t, with, 0)
	closeFloat(t, abs, 0)
	step(tt, now, 100*time.Millisecond) // 350ms
	closeFloat(t, overlap, 0)
	closeFloat(t, with, 50)
	closeFloat(t, abs, 0)
	step(tt, now, 100*time.Millisecond) // 450ms
	closeFloat(t, overlap, 0)
	closeFloat(t, with, 100)
	closeFloat(t, abs, 0)
	step(tt, now, 100*time.Millisecond) // 550ms
	closeFloat(t, overlap, 0)
	closeFloat(t, abs, 0)
	step(tt, now, 100*time.Millisecond) // 650ms
	closeFloat(t, overlap, 50)
	closeFloat(t, abs, 25)
	step(tt, now, 100*time.Millisecond) // 750ms
	closeFloat(t, overlap, 100)
	closeFloat(t, abs, 50)
}

func TestTimelineAbsolutePlacementAndDelay(t *testing.T) {
	var abs, delayed float32
	var dur time.Duration
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "abs", TimelineOptions{})
		abs = tl.To(Tween{From: 0, To: 100, Duration: 200 * time.Millisecond}, At(200*time.Millisecond))
		// Placed at 400ms and held at its From for another 100ms.
		delayed = tl.To(Tween{From: 10, To: 20, Duration: 200 * time.Millisecond, Delay: 100 * time.Millisecond},
			At(400*time.Millisecond))
		dur = tl.Duration()
	})
	if dur != 700*time.Millisecond {
		t.Fatalf("Duration = %v, want 700ms", dur)
	}
	closeFloat(t, abs, 0)
	closeFloat(t, delayed, 10)
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, abs, 0)               // it has not started yet
	step(tt, now, 200*time.Millisecond) // 300ms
	closeFloat(t, abs, 50)
	closeFloat(t, delayed, 10)
	step(tt, now, 200*time.Millisecond) // 500ms
	closeFloat(t, abs, 100)
	closeFloat(t, delayed, 10)          // the delay is over at 500ms
	step(tt, now, 100*time.Millisecond) // 600ms
	closeFloat(t, delayed, 15)
}

func TestTimelineLabels(t *testing.T) {
	var a, b, cv float32
	var dur time.Duration
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "labels", TimelineOptions{})
		a = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		tl.Label("intro") // where a ends, at 400ms
		b = tl.To(Tween{From: 0, To: 100, Duration: 200 * time.Millisecond}, AtLabel("intro"))
		tl.Label("far", At(800*time.Millisecond))
		cv = tl.To(Tween{From: 0, To: 100, Duration: 200 * time.Millisecond},
			AtLabel("far", 100*time.Millisecond))
		dur = tl.Duration()
	})
	if dur != 1100*time.Millisecond {
		t.Fatalf("Duration = %v, want 1.1s", dur)
	}
	closeFloat(t, b, 0)
	closeFloat(t, cv, 0)
	step(tt, now, 400*time.Millisecond)
	closeFloat(t, a, 100)
	closeFloat(t, b, 0)
	closeFloat(t, cv, 0)
	step(tt, now, 200*time.Millisecond) // 600ms
	closeFloat(t, b, 100)
	closeFloat(t, cv, 0)
	step(tt, now, 400*time.Millisecond) // 1s, 100ms into the cv tween
	closeFloat(t, cv, 50)
}

func TestTimelineEase(t *testing.T) {
	var linear, squared float32
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "ease", TimelineOptions{})
		linear = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		squared = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond, Ease: func(t float32) float32 { return t * t }},
			At(0))
	})
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, linear, 50)
	closeFloat(t, squared, 25) // the ease, not the time, shapes it
}

func TestTimelineTimeAndProgress(t *testing.T) {
	var tm time.Duration
	var p float32
	var done bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "progress", TimelineOptions{})
		tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		tm, p, done = tl.Time(), tl.Progress(), tl.Done()
	})
	if tm != 0 || p != 0 || done {
		t.Fatalf("a new timeline is at %v, %v, done %v", tm, p, done)
	}
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, float32(tm), float32(100*time.Millisecond))
	closeFloat(t, p, 0.25)
	step(tt, now, 500*time.Millisecond) // past the end
	closeFloat(t, float32(tm), float32(400*time.Millisecond))
	closeFloat(t, p, 1)
	if !done {
		t.Fatal("a timeline past its end is not done")
	}
	resting(t, tt)
}

func TestTimelinePauseAndResume(t *testing.T) {
	var x float32
	paused := false
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "pause", TimelineOptions{Paused: paused})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 25)
	paused = true
	tt.Frame()
	closeFloat(t, x, 25)
	resting(t, tt) // a paused timeline asks for no frames
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 25) // time passes and nothing moves
	paused = false
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 50) // it goes on where it was held
}

func TestTimelineSeekAndProgressHoldThePlayhead(t *testing.T) {
	var x float32
	var seek *time.Duration
	var progress *float32
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "seek", TimelineOptions{Seek: seek, Progress: progress})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 25)
	// A seek holds the playhead where the scrubber asks, and asks for no
	// frames while it does.
	where := 300 * time.Millisecond
	seek = &where
	tt.Frame()
	closeFloat(t, x, 75)
	resting(t, tt)
	step(tt, now, 200*time.Millisecond) // time passes and it stays there
	closeFloat(t, x, 75)
	// It goes on from where it was seeked to.
	seek = nil
	step(tt, now, 50*time.Millisecond)
	closeFloat(t, x, 87.5)
	// Progress does the same, by part of the duration.
	seek = nil
	part := float32(0.25)
	progress = &part
	tt.Frame()
	closeFloat(t, x, 25)
	resting(t, tt)
	progress = nil
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 50)
}

func TestTimelineReverse(t *testing.T) {
	var x float32
	reverse := false
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "reverse", TimelineOptions{Reverse: reverse})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 25)
	reverse = true
	tt.Frame()
	closeFloat(t, x, 75) // the same playhead, played backward
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 50)
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 0) // it comes to rest at its start
	resting(t, tt)
}

func TestTimelineRepeatAndAlternate(t *testing.T) {
	var x float32
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "repeat", TimelineOptions{Repeat: 1})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 50)
	step(tt, now, 200*time.Millisecond) // it plays again from the start
	closeFloat(t, x, 0)
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 50)
	step(tt, now, 200*time.Millisecond) // two plays, and it rests at the end
	closeFloat(t, x, 100)
	resting(t, tt)

	var y float32
	var tm time.Duration
	tt, now = timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "alternate", TimelineOptions{Repeat: 1, Alternate: true})
		y = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		tm = tl.Time()
	})
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, y, 50)
	step(tt, now, 200*time.Millisecond) // the second play goes backward
	closeFloat(t, y, 100)
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, y, 50)
	step(tt, now, 200*time.Millisecond) // two plays, and it rests where it began
	closeFloat(t, y, 0)
	if tm != 0 {
		t.Fatalf("Time = %v, want 0 at the end of an alternated timeline", tm)
	}
	resting(t, tt)
}

func TestTimelineRepeatForever(t *testing.T) {
	var x float32
	var done bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "forever", TimelineOptions{Repeat: RepeatForever})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		done = tl.Done()
	})
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 25)
	for range 20 {
		step(tt, now, 200*time.Millisecond)
	}
	if done {
		t.Fatal("a timeline that repeats for ever is done")
	}
	if !tt.rt.animating {
		t.Fatal("a timeline that repeats for ever stopped asking for frames")
	}
}

func TestTimelineStagger(t *testing.T) {
	var v [3]float32
	var dur time.Duration
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "stagger", TimelineOptions{})
		tl.Label("rows", At(0))
		for i := range v {
			v[i] = tl.To(Tween{From: 0, To: 1, Duration: 100 * time.Millisecond},
				AtLabel("rows", Stagger(i, 50*time.Millisecond)))
		}
		dur = tl.Duration()
	})
	if dur != 200*time.Millisecond {
		t.Fatalf("Duration = %v, want 200ms", dur)
	}
	closeFloat(t, v[0], 0)
	closeFloat(t, v[1], 0)
	closeFloat(t, v[2], 0)
	step(tt, now, 75*time.Millisecond)
	closeFloat(t, v[0], 0.75)
	closeFloat(t, v[1], 0.25)
	closeFloat(t, v[2], 0)
	step(tt, now, 50*time.Millisecond) // 125ms
	closeFloat(t, v[0], 1)
	closeFloat(t, v[1], 0.75)
	closeFloat(t, v[2], 0.25)
	step(tt, now, 100*time.Millisecond) // 225ms, past the end
	closeFloat(t, v[0], 1)
	closeFloat(t, v[1], 1)
	closeFloat(t, v[2], 1)
	resting(t, tt)
}

func TestTimelineCompletion(t *testing.T) {
	var x float32
	var done, arrived bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "done", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 1, Duration: 100 * time.Millisecond})
		done, arrived = tl.Done(), tl.Arrived()
	})
	if done || arrived {
		t.Fatal("a new timeline is done or arrived")
	}
	step(tt, now, 50*time.Millisecond)
	if done || arrived {
		t.Fatal("a timeline halfway through is done or arrived")
	}
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 1)
	if !done || !arrived {
		t.Fatalf("the frame it came to rest in: done %v, arrived %v", done, arrived)
	}
	resting(t, tt) // and it asks for no more frames
	step(tt, now, 100*time.Millisecond)
	if !done {
		t.Fatal("a timeline that came to rest is not done")
	}
	if arrived {
		t.Fatal("Arrived is true in a frame after the one it came to rest in")
	}
}

func TestTimelineRestartsWhenRunChanges(t *testing.T) {
	var x float32
	var run uint64
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "run", TimelineOptions{Run: run})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 300*time.Millisecond)
	closeFloat(t, x, 75)
	run = 1
	tt.Frame()
	closeFloat(t, x, 0) // it plays again from the start
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 50)
	// A run that stays the same does not restart it.
	step(tt, now, 100*time.Millisecond)
	closeFloat(t, x, 75)
}

func TestTimelineFollowsAChangedDefinition(t *testing.T) {
	var x, y float32
	two := false
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "definition", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		y = 0
		if two {
			y = tl.To(Tween{From: 0, To: 10, Duration: 200 * time.Millisecond})
		}
	})
	step(tt, now, 300*time.Millisecond)
	closeFloat(t, x, 75)
	// The view adds a tween: the playhead goes on, and the new tween shows
	// what its own time says, without jumping.
	two = true
	tt.Frame()
	closeFloat(t, x, 75)
	closeFloat(t, y, 0)
	step(tt, now, 150*time.Millisecond)
	closeFloat(t, x, 100)
	closeFloat(t, y, 2.5)
}

func TestTimelineLessMotion(t *testing.T) {
	var x float32
	var done bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "less", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
		done = tl.Done()
	})
	closeFloat(t, x, 0)
	tt.SetPreferences(Preferences{ReduceMotion: true})
	closeFloat(t, x, 100) // where it would have ended, at once
	if !done {
		t.Fatal("a timeline with less motion is not done")
	}
	resting(t, tt)
	// It stays at its end while the view goes on building it.
	step(tt, now, time.Second)
	closeFloat(t, x, 100)
	resting(t, tt)
}

func TestTimelineLessMotionRestarts(t *testing.T) {
	var x float32
	var run uint64
	tt, _ := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "less-run", TimelineOptions{Run: run})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	tt.SetPreferences(Preferences{ReduceMotion: true})
	closeFloat(t, x, 100)
	run = 1
	tt.Frame()
	closeFloat(t, x, 100)
	resting(t, tt)
}

func TestTimelineWithoutTweensStops(t *testing.T) {
	var done bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "empty", TimelineOptions{})
		done = tl.Done()
	})
	// Nothing is known to play yet: the next frame is the one that shows
	// the timeline has nothing to play.
	step(tt, now, time.Millisecond)
	if !done {
		t.Fatal("a timeline with no tweens never came to rest")
	}
	resting(t, tt)
}

func TestTimelineGoesWithTheView(t *testing.T) {
	var x float32
	build := true
	tt, now := timelineTester(func(c *context) {
		if !build {
			return
		}
		tl := coreNewTimeline(c, "gone", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 100, Duration: 400 * time.Millisecond})
	})
	step(tt, now, 200*time.Millisecond)
	closeFloat(t, x, 50)
	build = false
	tt.Frame()
	if _, ok := tt.rt.timelines["gone"]; ok {
		t.Fatal("a timeline the view stopped building stayed")
	}
	resting(t, tt)
}

func TestTimelineNilIsSafe(t *testing.T) {
	var tl *Timeline
	if got := tl.To(Tween{From: 3, To: 9, Duration: time.Second}); got != 3 {
		t.Fatalf("a nil Timeline showed %v, want its From", got)
	}
	tl.Label("nowhere")
	if tl.Duration() != 0 || tl.Time() != 0 || tl.Progress() != 0 || tl.Done() || tl.Arrived() {
		t.Fatal("a nil Timeline answered")
	}
}

func TestTimelinePanics(t *testing.T) {
	cases := []struct {
		name string
		view func(*context)
	}{
		{"a negative duration", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{}).To(Tween{From: 0, To: 1, Duration: -time.Millisecond})
		}},
		{"a negative delay", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{}).To(Tween{From: 0, To: 1, Duration: time.Second, Delay: -time.Millisecond})
		}},
		{"two placements", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{}).To(Tween{From: 0, To: 1, Duration: time.Second}, At(0), At(time.Second))
		}},
		{"a label that was not marked", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{}).To(Tween{From: 0, To: 1, Duration: time.Second}, AtLabel("nowhere"))
		}},
		{"a Repeat below RepeatForever", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{Repeat: -2}).To(Tween{From: 0, To: 1, Duration: time.Second})
		}},
		{"a negative timeline delay", func(c *context) {
			coreNewTimeline(c, "t", TimelineOptions{Delay: -time.Millisecond}).To(Tween{From: 0, To: 1, Duration: time.Second})
		}},
		{"two label offsets", func(c *context) {
			AtLabel("a", 0, time.Second)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			clockTester(tc.view, 100, 100)
		})
	}
}

func TestTimelineStaggerOffset(t *testing.T) {
	if got := Stagger(0, 30*time.Millisecond); got != 0 {
		t.Fatalf("Stagger(0) = %v, want 0", got)
	}
	if got := Stagger(3, 30*time.Millisecond); got != 90*time.Millisecond {
		t.Fatalf("Stagger(3) = %v, want 90ms", got)
	}
	if got := Stagger(-1, 30*time.Millisecond); got != 0 {
		t.Fatalf("Stagger(-1) = %v, want 0", got)
	}
}

func TestTimelinePlaysOnceAtOnce(t *testing.T) {
	// A tween that takes no time is at its To from the frame it is placed
	// in, and the timeline rests there.
	var x float32
	var done bool
	tt, now := timelineTester(func(c *context) {
		tl := coreNewTimeline(c, "instant", TimelineOptions{})
		x = tl.To(Tween{From: 0, To: 100})
		done = tl.Done()
	})
	step(tt, now, time.Millisecond)
	closeFloat(t, x, 100)
	if !done {
		t.Fatal("a timeline of one instant tween is not done")
	}
	resting(t, tt)
}
