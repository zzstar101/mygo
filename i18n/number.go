package i18n

import (
	"reflect"
	"strconv"
	"strings"
)

// number matches the numeric types Number writes.
type number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Number writes v the way the language of locale writes numbers: its decimal
// and group separators, its groups of digits (Hindi groups by two after the
// first three, as 12,34,567), and its digits (Arabic writes ١٢٣). A language
// i18n has no data for is written as English.
func Number[T number](locale string, v T) string {
	l, ok := languageOf(locale)
	if !ok {
		l = lang{decimal: ".", group: ",", groupSizes: []int{3}}
	}
	rv := reflect.ValueOf(v)
	var s string
	switch rv.Kind() {
	case reflect.Float32:
		s = strconv.FormatFloat(rv.Float(), 'f', -1, 32)
	case reflect.Float64:
		s = strconv.FormatFloat(rv.Float(), 'f', -1, 64)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s = strconv.FormatUint(rv.Uint(), 10)
	default:
		s = strconv.FormatInt(rv.Int(), 10)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteString(strings.Join(groupDigits(whole, l.groupSizes), l.group))
	if hasFrac {
		b.WriteString(l.decimal)
		b.WriteString(frac)
	}
	return l.digitsOf(b.String())
}

// groupDigits cuts digits into the groups the language writes them in: the
// sizes count from the right, the last of them for the groups before the
// first, as {3} for "1,234,567" and {3, 2} for "12,34,567".
func groupDigits(digits string, sizes []int) []string {
	if len(sizes) == 0 || len(digits) == 0 {
		return []string{digits}
	}
	groups := make([]string, 0, len(digits))
	for len(digits) > 0 {
		size := sizes[len(sizes)-1]
		if i := len(groups); i < len(sizes) {
			size = sizes[i]
		}
		if size >= len(digits) {
			groups = append(groups, digits)
			break
		}
		groups = append(groups, digits[len(digits)-size:])
		digits = digits[:len(digits)-size]
	}
	for i, j := 0, len(groups)-1; i < j; i, j = i+1, j-1 {
		groups[i], groups[j] = groups[j], groups[i]
	}
	return groups
}

// digitsOf writes the digits of s in the language's own digits, as Arabic
// writes ١٢٣.
func (l lang) digitsOf(s string) string {
	if l.digits == "" {
		return s
	}
	digits := []rune(l.digits)
	if len(digits) != 10 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(digits[r-'0'])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
