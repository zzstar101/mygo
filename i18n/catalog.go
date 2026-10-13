package i18n

import (
	"slices"
	"sync"
)

// Catalog holds the texts of an app in every language it has: a value per
// language tag, and the tag to fall back to when a locale suits none of
// them. Apps keep their own strings in one, and the native UI keeps the
// texts of its controls in one.
//
// The zero Catalog is empty: it has the texts of no language, and Get
// returns zero values.
type Catalog[T any] struct {
	mu       sync.RWMutex
	fallback string
	texts    map[string]*T
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
	return c
}

// Use adds the texts of the language tag, or replaces its texts.
func (c *Catalog[T]) Use(tag string, texts T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.texts == nil {
		c.texts = map[string]*T{}
	}
	c.texts[tag] = &texts
}

// UseAll adds the texts of every language texts names, or replaces them.
func (c *Catalog[T]) UseAll(texts map[string]T) {
	for tag, t := range texts {
		c.Use(tag, t)
	}
}

// Languages returns the tags of the languages it has texts of, sorted;
// with a fallback language among them.
func (c *Catalog[T]) Languages() []string {
	c.mu.RLock()
	tags := make([]string, 0, len(c.texts))
	for tag := range c.texts {
		tags = append(tags, tag)
	}
	c.mu.RUnlock()
	slices.Sort(tags)
	return tags
}

// Fallback returns the tag of the language Get uses when no language suits
// a locale.
func (c *Catalog[T]) Fallback() string { return c.fallback }

// Match returns the tag of the language whose texts suit locale, or "" when
// none does.
func (c *Catalog[T]) Match(locale string) string { return Match(locale, c.Languages()) }

// Get returns the texts of the language whose texts suit locale, or the
// fallback language's.
func (c *Catalog[T]) Get(locale string) *T {
	tag := c.Match(locale)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if t := c.texts[tag]; t != nil {
		return t
	}
	if t := c.texts[c.fallback]; t != nil {
		return t
	}
	return new(T)
}
