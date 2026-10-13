package ui

import (
	"slices"

	transfer "github.com/egoist/mygo/transfer"
)

// TextInput creates a single-line text input editing *value.
func coreTextInput(c *context, value *string) *node { return textInput(c, value, false) }

// TextArea creates a multi-line text input editing *value.
func coreTextArea(c *context, value *string) *node { return textInput(c, value, true) }

func textInput(c *context, value *string, multiline bool) *node {
	t := c.theme
	e := textInputBase(c, value, multiline)
	e.Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	if multiline {
		e.MinHeight(t.Space(20))
	}
	e.styleFn = func(e *node) { inputBorder(t, e, e) }
	return e
}

func textInputBase(c *context, value *string, multiline bool) *node {
	e := c.newElement(kindInput)
	e.flags |= flagEditable | flagFocusable | flagHover
	e.widget = "TextInput"
	if multiline {
		e.widget = "TextArea"
		// It scrolls its text as a scroll container does its children.
		e.flags |= flagScrollY
	}
	st := e.st
	if st.editor == nil || st.editor.document != nil {
		st.editor = newEditor()
		st.editor.setText(*value)
		st.editor.caret, st.editor.anchor = st.editor.buf.n, st.editor.buf.n
	}
	ed := st.editor
	ed.value = value

	if ed.client == nil {
		ed.client = &widgetTextInput{ed: ed, rt: c.rt, id: e.id}
	}
	e.textClient = ed.client
	ed.multiline = multiline
	if multiline && ed.area == nil {
		ed.area = &area{reveal: true}
	}
	// The input shares the string of *value: the same string is equal at
	// once, whatever its length.
	if !ed.nativeDirty && ed.buf.s != *value {
		ed.setText(*value)
		ed.compose = ""
	}
	ed.readOnly, ed.password, ed.lines, ed.onPaste, ed.onEnter = false, false, [2]int{}, nil, nil
	ed.ranges = ed.ranges[:0]
	e.onValueInput(stringInput)

	return e
}

// Placeholder shows s in an empty text input.
func (e *node) Placeholder(s string) *node {
	if e.st.editor != nil {
		e.st.editor.placeholder = s
	}
	return e
}

// ReadOnly makes a text input show its text without letting the user
// change it: the text can still be selected and copied, from the keyboard
// too, as it takes the focus, without a caret; assistive technology reads
// it as read-only.
func (e *node) ReadOnly(on bool) *node {
	if ed := e.st.editor; ed != nil && e.flags&flagEditable != 0 {
		ed.readOnly = on
		if on {
			ed.compose = ""
		}
	}
	return e
}

// OnPaste has fn see a paste into a text input, by Cmd+V, Ctrl+V or a
// Paste menu item, before it is inserted, on the main thread as the paste
// comes: text is the clipboard's text, empty when it holds none, as with
// an image, which the app reads from the clipboard itself. fn reports
// whether it took the paste; a paste taken changes nothing, its selection
// and undo history included, as when a web page prevents a paste's
// default and attaches a large block instead of inserting it. Like
// ReadOnly, it holds for the input that comes until the next frame.
func (e *node) OnPaste(fn func(text string) bool) *node {
	if ed := e.st.editor; ed != nil && e.flags&flagEditable != 0 {
		ed.onPaste = fn
	}
	return e
}

// OnEnter has fn see an Enter in the text input before the editor takes
// it, on the main thread as the key comes. fn reports whether it took the
// Enter; one taken changes nothing: a text area inserts no newline, and a
// single-line input does not submit. Like ReadOnly, it holds for the input
// that comes until the next frame.
//
// fn may run between frames, where an Element kept from the view is no
// longer valid: it reads and replaces the text through e instead, as a
// chat composer sends with Enter, clears what it sent, and breaks the line
// with Shift+Enter:
//
//	ui.TextArea(c, &app.draft).OnEnter(func(e ui.Enter) bool {
//		if e.Mods&ui.Shift != 0 {
//			return false
//		}
//		app.send(e.Text())
//		e.SetText("")
//		return true
//	})
func (e *node) OnEnter(fn func(e Enter) bool) *node {
	if ed := e.st.editor; ed != nil && e.flags&flagEditable != 0 {
		ed.onEnter = fn
	}
	return e
}

// Enter is an Enter pressed in a text input, as OnEnter's fn sees it. It
// is valid while fn runs.
type Enter struct {
	// Mods are the modifiers held with the Enter.
	Mods Modifiers
	ed   *editor
}

// Text returns the text of the input as the Enter comes, which may be
// ahead of the bound value: that takes what is typed as the next frame is
// built.
func (e Enter) Text() string {
	if e.ed == nil || !e.ed.entering {
		return ""
	}
	return e.ed.String()
}

// SetText replaces the text of the input, and its bound value, as the app
// does by setting the value as the view is built: a composer clears the
// draft it sent. Writing the bound value from fn is not enough, as the
// editor writes what it holds over it.
func (e Enter) SetText(s string) {
	ed := e.ed
	if ed == nil || !ed.entering {
		return
	}
	if ed.document != nil {
		// A TextBuffer takes the text as when the app sets it.
		ed.document.Set(s)
		ed.loadBuffer(ed.document.Snapshot())
		ed.bufferDirty = false
	} else {
		ed.setText(s)
		ed.compose = ""
		if ed.value != nil {
			*ed.value = s
		}
	}
	if w := ed.client; w != nil {
		// What the view built before the Enter shows the old text.
		w.rt.consumed = true
		w.rt.requestFrame()
	}
}

// Lines makes a text area as high as its text, wrapped at its width, from
// min lines up to max, past which it scrolls, as a message field grows with
// what is typed: TextArea(c, &draft).Lines(1, 8). Without it, a text area is
// as high as its paragraphs, three lines at least, unless given a height.
func (e *node) Lines(min, max int) *node {
	if ed := e.st.editor; ed != nil && ed.area != nil {
		ed.lines = [2]int{min, max}
	}
	return e
}

// TextSelection returns the selection of a text input as offsets in runes
// into its text, the caret where start equals end, as the app reads it to
// complete the word being typed.
func (e *node) TextSelection() (start, end int) {
	ed := e.st.editor
	if ed == nil {
		return 0, 0
	}
	return ed.selection()
}

// SetTextSelection selects the runes of a text input from start to end, or
// puts the caret at start when they are equal, and scrolls it into view: the
// caret after a word the app completed. The offsets are kept within the text.
func (e *node) SetTextSelection(start, end int) *node {
	ed := e.st.editor
	if ed == nil {
		return e
	}
	n := ed.buf.n
	start, end = min(max(start, 0), n), min(max(end, 0), n)
	ed.editorSelection = editorSelection{anchor: start, caret: end}
	ed.hasDesired = false
	if ed.area != nil {
		ed.area.reveal = true
	}
	e.c.rt.requestFrame()
	return e
}

// Composing reports whether an input method composes text in a text
// input, as Pinyin before a candidate is chosen: its value holds the text
// once composed. Keys typed meanwhile are the input method's, as Enter
// choosing a candidate: they submit nothing and press no shortcut.
func (e *node) Composing() bool {
	ed := e.st.editor
	return ed != nil && ed.compose != ""
}

// Password hides what a text input holds, in the frames that call it: an
// input that stops calling it shows its text again, as a field's eye
// button does. It does nothing to a text area: as on every platform, only
// single-line fields hide their text.
func (e *node) Password() *node {
	if ed := e.st.editor; ed != nil && !ed.multiline {
		ed.password = true
	}
	return e
}

// Selectable lets the user select and copy text by dragging, double-clicking
// a word, triple-clicking a line, or using Shift with the arrows. Its context
// menu has Copy and Select All; Cmd+C copies the selection.
//
// On a container, its Text and RichText descendants share one selection.
// Copy joins their selected text with newlines, in the order they were built.
// Nested Selectable containers have independent selections. Text inside
// controls, such as buttons and text inputs, keeps the control's interaction.
// Unselectable excludes a subtree. On a Text or RichText alone, the selection
// stays within that paragraph. Inline elements share their paragraph's
// selection; set Selectable on the paragraph.
func (e *node) Selectable() *node {
	if e.isInline() {
		return e
	}
	if e.kind == kindText || e.kind == kindBox {
		e.flags &^= flagUnselectable
		e.flags |= flagSelectable
	}
	return e
}

// SelectedText is the part of a paragraph that the selection of a
// Selectable container holds.
type SelectedText struct {
	// ID is the paragraph's element's (Element.ID).
	ID uint64
	// Text is the paragraph's text, and Start and End the runes of it the
	// selection holds.
	Text       string
	Start, End int
}

// SelectedTexts returns the parts of the paragraphs that the selection of
// a Selectable container holds, in the order they were built, or none,
// as of the last frame: to copy them otherwise, as a context menu's item
// does.
func (e *node) SelectedTexts() []SelectedText {
	return e.c.rt.selectedTexts(e.id)
}

// CopyData sets what Copy, its shortcut and the context menu's put on
// the clipboard for the selection of a Selectable container: fn gets its
// parts, as SelectedTexts returns them, and returns the data, as a
// document copies its paragraphs with blank lines between them as text
// (transfer.TextData), and as HTML too to keep their formatting.
func (e *node) CopyData(fn func(parts []SelectedText) transfer.Data) *node {
	e.copyData = fn
	return e
}

// Unselectable excludes a paragraph or container subtree from a surrounding
// Selectable container. A Selectable container nested inside it can provide
// a selection of its own. For inline elements, set it on their paragraph.
func (e *node) Unselectable() *node {
	if e.isInline() {
		return e
	}
	e.flags &^= flagSelectable
	e.flags |= flagUnselectable
	return e
}

// TextRange styles runes Start up to End of a text input's text: their
// color and, when Weight is set, their weight, as a message field shows
// the mentions in what is typed.
type TextRange struct {
	Start, End int
	Color      Color
	Weight     int
}

// TextRanges styles runs of a text input's text, in the frames that call
// it. Where ranges overlap, the one starting first shows, or given first
// when they start together. They follow the text as it is: an app that
// finds them in the text finds them again as it changes. A password shows
// none; an input method's composition shows unstyled between them.
func (e *node) TextRanges(ranges ...TextRange) *node {
	ed := e.st.editor
	if ed == nil || e.flags&flagEditable == 0 {
		return e
	}
	for _, r := range ranges {
		if r.End > r.Start {
			ed.ranges = append(ed.ranges, r)
			ed.apart = false
		}
	}
	return e
}

// sortRanges sorts the ranges by start and clips where they overlap, as
// TextRanges says, once the frame has given them all: a frame calling
// TextRanges for each range sorts them once, and clipping ranges given
// earlier would let a later one overlapping them win.
func (ed *editor) sortRanges() {
	slices.SortStableFunc(ed.ranges, func(a, b TextRange) int { return a.Start - b.Start })
	apart := ed.ranges[:0]
	for _, r := range ed.ranges {
		if n := len(apart); n > 0 {
			r.Start = max(r.Start, apart[n-1].End)
		}
		if r.End > r.Start {
			apart = append(apart, r)
		}
	}
	ed.ranges, ed.apart = apart, true
}
