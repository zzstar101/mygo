package i18n

import (
	"strconv"
	"strings"
	"time"
)

// Style picks how much of a date Date writes.
type Style uint8

// Date styles, from the shortest to the longest.
const (
	// Short writes a date in numbers alone, as "3/5/26" and "05.03.26".
	Short Style = iota
	// Medium writes a date with the month's short name, as "Mar 5, 2026"
	// and "5 mars 2026".
	Medium
	// Long writes a date with the month's name and the day of the week, as
	// "Thursday, March 5, 2026" and "jeudi 5 mars 2026".
	Long
)

// Date writes t as the language of locale writes dates, in the digits of that
// language: "Thursday, March 5, 2026", "5 mars 2026" and "الخميس، ٥ مارس ٢٠٢٦".
// A language i18n has no data for is written as "2006-01-02", whatever the
// style.
func Date(locale string, t time.Time, style Style) string {
	l, ok := languageOf(locale)
	if !ok {
		return t.Format("2006-01-02")
	}
	pattern := l.dateMedium
	switch style {
	case Short:
		pattern = l.dateShort
	case Long:
		pattern = l.dateLong
	}
	return l.digitsOf(formatDate(pattern, t, l))
}

// Time writes the time of day of t as the language of locale writes it, with
// its 12 or 24 hour clock and its own words for the morning and the
// afternoon: "5:04 PM", "17:04", "下午5:04". A language i18n has no data for
// is written as "15:04".
func Time(locale string, t time.Time) string {
	l, ok := languageOf(locale)
	if !ok {
		return t.Format("15:04")
	}
	return l.digitsOf(formatDate(l.time, t, l))
}

// formatDate writes t with a pattern of the language: the letters y (year),
// M (month), d (day), E (day of the week), H (hour, 24 hour clock), h (hour,
// 12 hour clock), m (minute), s (second) and a (morning or afternoon),
// repeated as the language repeats them, as "yyyy", "MMMM" and "EEEE", and
// single quotes around literal text.
func formatDate(pattern string, t time.Time, l lang) string {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		c := pattern[i]
		switch {
		case c == '\'':
			// Two single quotes are one literal single quote, inside a
			// quoted part or outside it.
			if i+1 < len(pattern) && pattern[i+1] == '\'' {
				b.WriteByte('\'')
				i += 2
				continue
			}
			// A quoted part is literal text, whatever it holds.
			i++
			for i < len(pattern) {
				if pattern[i] != '\'' {
					b.WriteByte(pattern[i])
					i++
					continue
				}
				if i+1 < len(pattern) && pattern[i+1] == '\'' {
					b.WriteByte('\'')
					i += 2
					continue
				}
				i++
				break
			}
		case isDateLetter(c):
			j := i
			for j < len(pattern) && pattern[j] == c {
				j++
			}
			b.WriteString(dateToken(c, j-i, t, l))
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// isDateLetter reports whether c is a letter of a date pattern.
func isDateLetter(c byte) bool {
	return c == 'y' || c == 'M' || c == 'd' || c == 'E' || c == 'H' || c == 'h' || c == 'm' || c == 's' || c == 'a'
}

// dateToken writes one field of a date pattern, repeated n times.
func dateToken(c byte, n int, t time.Time, l lang) string {
	switch c {
	case 'y':
		if n == 2 {
			return pad(t.Year()%100, 2)
		}
		if n >= 4 {
			return pad(t.Year(), 4)
		}
		return strconv.Itoa(t.Year())
	case 'M':
		switch {
		case n >= 4:
			if name := l.monthsLong[t.Month()-1]; name != "" {
				return name
			}
		case n == 3:
			if name := l.monthsShort[t.Month()-1]; name != "" {
				return name
			}
		}
		if n == 2 {
			return pad(int(t.Month()), 2)
		}
		return strconv.Itoa(int(t.Month()))
	case 'd':
		if n == 2 {
			return pad(t.Day(), 2)
		}
		return strconv.Itoa(t.Day())
	case 'E':
		// i18n knows the names of the days of the week, not their short
		// forms, and writes the names for both.
		return l.weekdays[int(t.Weekday())]
	case 'H':
		if n == 2 {
			return pad(t.Hour(), 2)
		}
		return strconv.Itoa(t.Hour())
	case 'h':
		hour := t.Hour() % 12
		if hour == 0 {
			hour = 12
		}
		if n == 2 {
			return pad(hour, 2)
		}
		return strconv.Itoa(hour)
	case 'm':
		if n == 2 {
			return pad(t.Minute(), 2)
		}
		return strconv.Itoa(t.Minute())
	case 's':
		if n == 2 {
			return pad(t.Second(), 2)
		}
		return strconv.Itoa(t.Second())
	case 'a':
		if t.Hour() < 12 {
			return l.am
		}
		return l.pm
	}
	return ""
}

// pad writes n with at least width digits.
func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
