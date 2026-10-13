package i18n

import (
	"reflect"
	"slices"
	"sync"
)

// Catalog holds the texts of an app in every language it has: a value per
// language tag, and the tag to fall back to when a locale suits none of
// them. Apps keep their own strings in one, and the native UI keeps the
// texts of its controls in one. It is safe from any goroutine.
//
// The zero Catalog is empty: it has the texts of no language, and Get
// returns zero values.
type Catalog[T any] struct {
	mu       sync.RWMutex
	fallback string
	texts    map[string]*T
	// tags are the tags of texts, sorted, and parsed the parts of each, for
	// Get to match a locale without taking them apart every time.
	tags   []string
	parsed []tag
}

// New returns a Catalog of texts whose fallback language is fallback, the
// tag of one of texts: the language Get uses when no language suits a
// locale. A fallback no language has is added as a zero value, so that Get
// always has texts to return.
func New[T any](fallback string, texts map[string]T) *Catalog[T] {
	c := &Catalog[T]{fallback: fallback, texts: make(map[string]*T, len(texts)+1)}
	for tag, t := range texts {
		c.texts[tag] = &t
	}
	if _, ok := c.texts[fallback]; !ok {
		var zero T
		c.texts[fallback] = &zero
	}
	c.index()
	return c
}

// index lists the tags of the texts, sorted, and their parts. c.mu must be
// held, or c not shared yet.
func (c *Catalog[T]) index() {
	c.tags = c.tags[:0]
	for tag := range c.texts {
		c.tags = append(c.tags, tag)
	}
	slices.Sort(c.tags)
	c.parsed = c.parsed[:0]
	for _, tag := range c.tags {
		c.parsed = append(c.parsed, parse(tag))
	}
}

// Use adds the texts of the language tag, or replaces its texts.
func (c *Catalog[T]) Use(tag string, texts T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.use(tag, texts)
}

// use is Use with c.mu held.
func (c *Catalog[T]) use(tag string, texts T) {
	if c.texts == nil {
		c.texts = map[string]*T{}
	}
	_, known := c.texts[tag]
	c.texts[tag] = &texts
	if !known {
		c.index()
	}
}

// UseAll adds the texts of every language texts names, or replaces them,
// at once: Get returns the texts before or the texts after, never some of
// each.
func (c *Catalog[T]) UseAll(texts map[string]T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for tag, t := range texts {
		c.use(tag, t)
	}
}

// Change changes the texts of the languages changes names, at once: the
// texts of a tag become those Get returns for it with the fields of its
// change that are not their zero value, so a change says only what it
// changes. A tag of a language the catalog has none of adds it, with the
// fallback's texts for what the change leaves out; a tag of a region, as
// "zh-Hant-TW", adds the texts of its language, as zh-Hant, with the
// change. T must be a struct.
func (c *Catalog[T]) Change(changes map[string]T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for tag, change := range changes {
		t := *c.get(tag)
		to, from := reflect.ValueOf(&t).Elem(), reflect.ValueOf(change)
		for i := range to.NumField() {
			if f := from.Field(i); !f.IsZero() && to.Field(i).CanSet() {
				to.Field(i).Set(f)
			}
		}
		c.use(tag, t)
	}
}

// Languages returns the tags of the languages it has texts of, sorted;
// with a fallback language among them.
func (c *Catalog[T]) Languages() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.tags)
}

// Fallback returns the tag of the language Get uses when no language suits
// a locale.
func (c *Catalog[T]) Fallback() string { return c.fallback }

// Match returns the tag of the language whose texts suit locale, or "" when
// none does.
func (c *Catalog[T]) Match(locale string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.match(locale)
}

// match is Match with c.mu held.
func (c *Catalog[T]) match(locale string) string {
	want := parse(locale)
	if want.lang == "" {
		return ""
	}
	best, bestScore := "", -1
	for i, a := range c.parsed {
		if score := want.score(a); score > bestScore {
			best, bestScore = c.tags[i], score
		}
	}
	return best
}

// Get returns the texts of the language whose texts suit locale, or the
// fallback language's. The texts are the catalog's own: read them, and
// change them with Use or Change.
func (c *Catalog[T]) Get(locale string) *T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.get(locale)
}

// get is Get with c.mu held.
func (c *Catalog[T]) get(locale string) *T {
	if t := c.texts[c.match(locale)]; t != nil {
		return t
	}
	if t := c.texts[c.fallback]; t != nil {
		return t
	}
	return new(T)
}
