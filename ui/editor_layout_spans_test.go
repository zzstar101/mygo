package ui

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// The ranges in reach of a paragraph, found by their starts, style it as
// all of the ranges do, overlapping as an app's may, and with an input
// method composing in or near it.
func TestRangeSpansSame(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 4000 {
		var ranges []TextRange
		for at := rng.IntN(5); at < 400; {
			n := 1 + rng.IntN(8)
			r := TextRange{Start: at, End: at + n}
			if rng.IntN(2) == 0 {
				r.Color = RGBA(1, 0, 0, 1)
			}
			if rng.IntN(3) == 0 {
				r.Weight = 700
			}
			ranges = append(ranges, r)
			at += n + rng.IntN(6)
			if rng.IntN(2) == 0 {
				at -= rng.IntN(2 * n) // the next starts inside this one, or before it
			}
			at = max(at, 0)
		}
		ed := &editor{ranges: ranges}
		ed.sortRanges()
		start := rng.IntN(420)
		end := start + rng.IntN(60)
		if rng.IntN(2) == 0 {
			ed.compose, ed.caret = "かな", max(0, start-2+rng.IntN(end-start+5))
		}
		k1, p1 := ed.rangeSpans(start, end)
		k2, p2 := ed.spansOf(ed.ranges, start, end)
		if k1 != k2 || !reflect.DeepEqual(p1, p2) {
			t.Fatalf("ranges %v, %d to %d, composing %d runes at %d: %q %+v, want %q %+v",
				ed.ranges, start, end, utf8.RuneCountInString(ed.compose), ed.caret, k1, p1, k2, p2)
		}
	}
}

// Where ranges overlap, the one starting first shows, or given first when
// they start together, and the other from where it ends.
func TestSortRangesKeepsThemApart(t *testing.T) {
	red, blue := RGBA(1, 0, 0, 1), RGBA(0, 0, 1, 1)
	ed := &editor{ranges: []TextRange{
		{Start: 5, End: 20, Weight: 700},
		{Start: 0, End: 10, Color: red},
		{Start: 12, End: 15, Color: blue}, // inside the bold one: hidden
		{Start: 30, End: 34, Color: red},
		{Start: 30, End: 32, Color: blue}, // given after the red one
	}}
	ed.sortRanges()
	want := []TextRange{
		{Start: 0, End: 10, Color: red},
		{Start: 10, End: 20, Weight: 700},
		{Start: 30, End: 34, Color: red},
	}
	if !reflect.DeepEqual(ed.ranges, want) {
		t.Fatalf("kept %v, want %v", ed.ranges, want)
	}
}

// A frame calling TextRanges for each range, as the docs' mentions do,
// styles the text as one call with all of them: a range given later does
// not win over an earlier one clipped by a third.
func TestTextRangesCalledForEach(t *testing.T) {
	red, blue, green := RGB(220, 0, 0), RGB(0, 0, 220), RGB(0, 160, 0)
	given := []TextRange{
		{Start: 0, End: 10, Color: red},
		{Start: 5, End: 20, Color: blue},
		{Start: 8, End: 15, Color: green}, // inside the blue one once it is clipped
	}
	text := strings.Repeat("abcd ", 6)
	var each, once *node
	coreNewTester(func(c *context) {
		coreColumn(c).Children(func() {
			each = coreTextAreaBase(c, &text).Width(300)
			for _, r := range given {
				each.TextRanges(r)
			}
			once = coreTextAreaBase(c, &text).Width(300).TextRanges(given...)
		})
	}, 320, 240)
	want := []TextRange{{Start: 0, End: 10, Color: red}, {Start: 10, End: 20, Color: blue}}
	if got := each.st.editor.ranges; !reflect.DeepEqual(got, want) {
		t.Errorf("a call for each range kept %v, want %v", got, want)
	}
	if got := once.st.editor.ranges; !reflect.DeepEqual(got, want) {
		t.Errorf("one call kept %v, want %v", got, want)
	}
}

// BenchmarkRangeSpans styles every line of a 3,000-line text with ten
// ranges a line, as a highlighted script has: by the ranges in reach of
// each line, and by walking all of them.
func BenchmarkRangeSpans(b *testing.B) {
	const lines, perLine, width = 3000, 10, 40
	ed := &editor{}
	for l := range lines {
		for i := range perLine {
			at := l*width + i*4
			ed.ranges = append(ed.ranges, TextRange{Start: at, End: at + 3, Color: RGBA(1, 0, 0, 1)})
		}
	}
	ed.sortRanges()
	for _, c := range []struct {
		name string
		f    func(start, end int) (string, *spanPaint)
	}{
		{"search", ed.rangeSpans},
		{"every", func(start, end int) (string, *spanPaint) { return ed.spansOf(ed.ranges, start, end) }},
	} {
		b.Run(c.name, func(b *testing.B) {
			for b.Loop() {
				for l := range lines {
					c.f(l*width, l*width+width-1)
				}
			}
		})
	}
}

// BenchmarkTextAreaRanges builds, lays out and paints the frames of a text
// area of 3,000 lines with ten ranges a line, given by a TextRanges call
// for each as the docs' mentions are.
func BenchmarkTextAreaRanges(b *testing.B) {
	const lines, perLine, width = 3000, 10, 40
	text := strings.Repeat(strings.Repeat("abc ", width/4)[:width-1]+"\n", lines)
	var ranges []TextRange
	for l := range lines {
		for i := range perLine {
			at := l*width + i*4
			ranges = append(ranges, TextRange{Start: at, End: at + 3, Color: RGB(220, 0, 0)})
		}
	}
	tt := coreNewTester(func(c *context) {
		a := coreTextAreaBase(c, &text).Width(600).Height(400)
		for _, r := range ranges {
			a.TextRanges(r)
		}
	}, 640, 480)
	for b.Loop() {
		tt.h.img.Invalidate()
		tt.Frame()
	}
}
