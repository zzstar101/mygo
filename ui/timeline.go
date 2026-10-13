package ui

import "time"

// A Timeline plays several tweens along one playhead, so that motion that
// belongs together is written, controlled and tested together: a card
// arriving while its title fades in, rows following one another, a small
// bounce after the card lands. It coordinates the motion already in MyGo:
// a tween returns the value it shows in this frame, and the view gives it
// to a transform, a pose or a value, as it would give one of AnimateTo's.
//
//	tl := ui.NewTimeline(c, "intro", ui.TimelineOptions{Run: app.opened})
//	x := tl.To(ui.Tween{From: -200, To: 0, Duration: 400 * time.Millisecond, Ease: ui.EaseOut})
//	y := tl.To(ui.Tween{From: 20, To: 0, Duration: 300 * time.Millisecond},
//		ui.OverlapPrevious(150*time.Millisecond))
//	card.Translate(x, y)
//
// The timeline of a key is the same timeline from frame to frame while the
// view builds it, and goes when the view stops, as an element's state does:
// build it in the same order every frame, and change Run to play it again,
// or a changed one.
type Timeline struct {
	c         *context
	st        *timelineState
	segs      []tweenSeg
	prevStart time.Duration
	prevEnd   time.Duration
}

// A Tween is a value going from From to To along Ease over Duration, which
// a Timeline places and plays. With no Ease it moves at one speed.
type Tween struct {
	From, To float32
	Duration time.Duration
	Ease     Easing
	// Delay holds the tween at its From for a while after where it was
	// placed.
	Delay time.Duration
}

// TimelineOptions says how a timeline plays. What it changes applies to the
// frame it is given in, except Run, which plays the timeline over when it
// changes, as KeyframeOptions.Run does.
type TimelineOptions struct {
	// Run restarts the timeline when it changes, so that a view plays it
	// again by counting: the value a callback or a piece of state gives.
	Run uint64
	// Repeat is how many more times the timeline plays after the first,
	// RepeatForever for as long as the view builds it.
	Repeat int
	// Alternate plays every other time backward.
	Alternate bool
	// Delay holds the timeline at its first value for a while before it
	// plays, and RepeatDelay the value it got to between plays.
	Delay, RepeatDelay time.Duration
	// Paused holds the playhead where it is, and Reverse plays it
	// backward, to its start.
	Paused  bool
	Reverse bool
	// Seek holds the playhead at a time of the timeline, and Progress at
	// a part of its duration, for a scrubber or a jump: while either is
	// set the timeline does not play, and it goes on from there when it
	// is not.
	Seek     *time.Duration
	Progress *float32
}

// NewTimeline returns the timeline this window keeps under key, playing it
// as o says. Every tween added to it shares one playhead, and returns the
// value it shows in this frame.
//
//	tl := ui.NewTimeline(c, "intro", ui.TimelineOptions{Run: app.opened})
//	x := tl.To(ui.Tween{From: -200, To: 0, Duration: 400 * time.Millisecond, Ease: ui.EaseOut})
//	card.Translate(x, 0)
//
// Key tells apart the timelines of one window, so it must be stable from
// frame to frame and the same in no two timelines. Play the timeline again,
// or a changed one, by changing o.Run.
func coreNewTimeline(c *context, key any, o TimelineOptions) *Timeline {
	rt := c.rt
	if rt.timelines == nil {
		rt.timelines = map[any]*timelineState{}
	}
	st := rt.timelines[key]
	if st == nil {
		st = &timelineState{}
		rt.timelines[key] = st
	}
	tl := st.handle
	if tl == nil {
		tl = &Timeline{}
		st.handle = tl
	}
	tl.c, tl.st = c, st
	tl.segs = st.segs[:0]
	tl.prevStart, tl.prevEnd = 0, 0
	tl.begin(o)
	return tl
}

// To adds tw to the timeline at at, which with no placement follows the
// tween before it at once, and returns the value it shows in this frame.
// Before the tween's time it shows its From, after it its To.
//
//	x := tl.To(ui.Tween{From: 0, To: 120, Duration: 300 * time.Millisecond})
//	y := tl.To(ui.Tween{From: 20, To: 0, Duration: 200 * time.Millisecond},
//		ui.OverlapPrevious(150*time.Millisecond))
func (t *Timeline) To(tw Tween, at ...Placement) float32 {
	if t == nil {
		return tw.From
	}
	if tw.Duration < 0 {
		panic("ui: animation durations must not be negative")
	}
	if tw.Delay < 0 {
		panic("ui: animation durations must not be negative")
	}
	start := t.place(at) + tw.Delay
	seg := tweenSeg{start: start, dur: tw.Duration, from: tw.From, to: tw.To, ease: tw.Ease}
	t.segs = append(t.segs, seg)
	t.st.segs = t.segs
	if end := start + tw.Duration; end > t.st.build {
		t.st.build = end
	}
	t.prevStart, t.prevEnd = start, start+tw.Duration
	return t.show(seg)
}

// Label marks a time of the timeline for a tween to start at with AtLabel.
// With no placement it marks where the tween before it ends.
//
//	tl.Label("intro", ui.At(0))
//	...
//	tl.To(ui.Tween{...}, ui.AtLabel("intro", 200*time.Millisecond))
func (t *Timeline) Label(name string, at ...Placement) {
	if t == nil {
		return
	}
	t.st.labels = append(t.st.labels, timelineLabel{name: name, at: t.place(at)})
}

// Duration is how long the timeline plays: the end of its last tween,
// without its repeats.
func (t *Timeline) Duration() time.Duration {
	if t == nil {
		return 0
	}
	if t.st.build > t.st.duration {
		return t.st.build
	}
	return t.st.duration
}

// Time is where the playhead is, from the start of the timeline.
func (t *Timeline) Time() time.Duration {
	if t == nil {
		return 0
	}
	if t.st.over {
		return t.Duration()
	}
	if d := t.Duration(); t.st.t > d {
		return d
	}
	return t.st.t
}

// Progress is how far the playhead is along the timeline, from 0 to 1.
func (t *Timeline) Progress() float32 {
	if t == nil {
		return 0
	}
	if t.st.over || t.st.done && t.Duration() <= 0 {
		return 1
	}
	d := t.Duration()
	if d <= 0 {
		return 0
	}
	p := float32(t.st.t) / float32(d)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// Done reports whether the timeline came to rest at its end, its repeats
// played out, for the view to go on as with Element.Settled.
func (t *Timeline) Done() bool {
	return t != nil && t.st.done
}

// Arrived is true in the one frame in which the timeline came to rest, for
// the view to go on, as Element.Arrived is.
func (t *Timeline) Arrived() bool {
	return t != nil && t.st.rested != 0 && t.st.rested == t.c.rt.frame
}

// place resolves where a tween or label starts, in at most one placement.
func (t *Timeline) place(at []Placement) time.Duration {
	if len(at) > 1 {
		panic("ui: a Timeline placement takes at most one Placement")
	}
	var p Placement
	if len(at) == 1 {
		p = at[0]
	}
	var pos time.Duration
	switch p.kind {
	case placeWith:
		pos = t.prevStart
	case placeOverlap:
		pos = t.prevEnd - p.at
	case placeAt:
		pos = p.at
	case placeLabel:
		pos = t.label(p.label) + p.at
	default:
		pos = t.prevEnd + p.at
	}
	if pos < 0 {
		// Nothing starts before the timeline does: an overlap larger
		// than what came before it starts at zero.
		pos = 0
	}
	return pos
}

// label returns where a label of this frame's timeline was marked.
func (t *Timeline) label(name string) time.Duration {
	labels := t.st.labels
	for i := len(labels) - 1; i >= 0; i-- {
		if labels[i].name == name {
			return labels[i].at
		}
	}
	panic("ui: a Timeline has no label " + name)
}

// show returns the value a segment shows at this frame's playhead: 0 to 1
// of its time along its Ease, between its From and its To.
func (t *Timeline) show(s tweenSeg) float32 {
	st := t.st
	if st.over {
		// Less motion: every tween is where it ends at once.
		return s.to
	}
	now := st.t
	if now <= s.start {
		return s.from
	}
	if s.dur <= 0 || now >= s.start+s.dur {
		return s.to
	}
	p := float32(now-s.start) / float32(s.dur)
	if p >= 1 {
		return s.to
	}
	ease := s.ease
	if ease == nil {
		ease = Linear
	}
	return lerp(s.from, s.to, ease(p))
}

// begin starts a frame's build of the timeline: at the start of a frame it
// takes what the last one built and moves the playhead, and at the start of
// a pass it starts the segments and labels over, as building always does.
func (t *Timeline) begin(o TimelineOptions) {
	st, rt := t.st, t.c.rt
	if st.frame != rt.frame {
		st.frame, st.pass = rt.frame, rt.pass
		st.duration, st.build = st.build, 0
		st.segs, st.labels = st.segs[:0], st.labels[:0]
		t.segs = st.segs
		t.advance(o)
		return
	}
	if st.pass != rt.pass {
		st.pass = rt.pass
		st.build = 0
		st.segs, st.labels = st.segs[:0], st.labels[:0]
		t.segs = st.segs
	}
	t.prevStart, t.prevEnd = 0, 0
}

// advance moves the playhead to this frame's time and asks for the frame
// that goes on, once per frame, as long as the timeline moves.
func (t *Timeline) advance(o TimelineOptions) {
	st, c := t.st, t.c
	if o.Repeat < RepeatForever || o.Delay < 0 || o.RepeatDelay < 0 {
		panic("ui: a Timeline Repeat must be RepeatForever or nonnegative, and its delays nonnegative")
	}
	now := c.now
	known := st.started
	if !st.started {
		st.started, st.at, st.run = true, now, o.Run
	} else {
		dt := now.Sub(st.at)
		st.at = now
		if dt > 0 && !o.Paused {
			st.raw += dt
		}
		if st.run != o.Run {
			st.run, st.raw, st.done, st.over, st.rested = o.Run, 0, false, false, 0
		}
	}
	if c.rt.preferences().ReduceMotion {
		// Values and poses go to their targets at once, and a sequence
		// ends at its last value: the timeline is over where it would
		// have ended, in this frame.
		st.over = true
	}
	if st.over {
		st.t, st.done = st.duration, true
		if st.rested == 0 {
			st.rested = c.rt.frame
		}
		return
	}
	raw, held := st.raw-o.Delay, false
	if o.Seek != nil {
		raw, held = *o.Seek, true
	}
	if o.Progress != nil {
		raw, held = time.Duration(float64(*o.Progress)*float64(st.duration)), true
	}
	if held {
		// Hold the playhead where the scrubber asks and go on from
		// there when it lets go.
		st.raw = raw + o.Delay
	}
	st.t, _, st.done = playhead(raw, st.duration, o.Repeat, o.Alternate, o.RepeatDelay)
	if raw > 0 && st.duration <= 0 {
		// A timeline whose tweens take no time is over as soon as a
		// frame has gone by: the frame it is first built in has no
		// duration yet to be over.
		st.done = known
	}
	if o.Reverse {
		if st.t = st.duration - st.t; st.t < 0 {
			st.t = 0
		}
	}
	if st.done {
		if st.rested == 0 {
			st.rested = c.rt.frame
		}
		return
	}
	if !o.Paused && !held {
		c.AnimationFrame()
	}
}

// A Placement says where along the timeline a tween or a label starts: at
// a time, at a label, or relative to what came before it. With no
// placement, what comes next follows the one before it at once.
//
//	tl.To(ui.Tween{...}, ui.At(500*time.Millisecond))
//	tl.To(ui.Tween{...}, ui.OverlapPrevious(150*time.Millisecond))
//	tl.To(ui.Tween{...}, ui.AtLabel("intro", 200*time.Millisecond))
type Placement struct {
	kind  placeKind
	at    time.Duration
	label string
}

// placeKind is what a Placement measures from.
type placeKind uint8

const (
	placeAfter placeKind = iota // after the one before it, which is nothing
	placeWith
	placeOverlap
	placeAt
	placeLabel
)

// AfterPrevious starts a tween d after the one before it ends.
func AfterPrevious(d time.Duration) Placement { return Placement{kind: placeAfter, at: d} }

// OverlapPrevious starts a tween d before the one before it ends.
func OverlapPrevious(d time.Duration) Placement { return Placement{kind: placeOverlap, at: d} }

// WithPrevious starts a tween where the one before it starts, to move
// something along with it.
func WithPrevious() Placement { return Placement{kind: placeWith} }

// At starts a tween d into the timeline.
func At(d time.Duration) Placement { return Placement{kind: placeAt, at: d} }

// AtLabel starts a tween at a Label of the timeline, offset by offset.
func AtLabel(label string, offset ...time.Duration) Placement {
	if len(offset) > 1 {
		panic("ui: AtLabel takes at most one offset")
	}
	p := Placement{kind: placeLabel, label: label}
	if len(offset) == 1 {
		p.at = offset[0]
	}
	return p
}

// Stagger returns how long the i-th element of a staggered row waits: i
// times step, and nothing for the first. Give it to the placement of each
// tween, as they start from one label and follow each other:
//
//	for i, row := range rows {
//		rows[i].Opacity(tl.To(ui.Tween{From: 0, To: 1, Duration: 200 * time.Millisecond},
//			ui.AtLabel("rows", ui.Stagger(i, 40*time.Millisecond))))
//	}
func Stagger(i int, step time.Duration) time.Duration {
	if i <= 0 {
		return 0
	}
	return time.Duration(i) * step
}

// tweenSeg is one tween as the timeline placed it.
type tweenSeg struct {
	start, dur time.Duration
	from, to   float32
	ease       Easing
}

// timelineState is what a Timeline keeps between frames: how long its
// playhead has run, and the segments and labels its last frame built,
// which this frame's playhead folds around. The engine keeps one per key
// (timelines) and drops it when a frame does not build it.
type timelineState struct {
	handle   *Timeline
	frame    uint64        // the frame the playhead last moved in
	pass     int           // the pass it was built in, to build once per pass
	at       time.Time     // the time of that frame's clock
	started  bool          // whether it has a time to measure from
	run      uint64        // the Run it plays, to restart when it changes
	raw      time.Duration // how long it has played, unrolled
	duration time.Duration // the last frame's Duration, which folds this one
	build    time.Duration // the Duration this frame is building
	t        time.Duration // where the playhead is
	done     bool          // it came to rest
	over     bool          // less motion: it settled at once
	rested   uint64        // the frame it came to rest in, for Arrived
	segs     []tweenSeg
	labels   []timelineLabel
}

// timelineLabel is a label a frame marked on a timeline.
type timelineLabel struct {
	name string
	at   time.Duration
}
