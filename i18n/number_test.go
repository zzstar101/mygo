package i18n

import "testing"

func TestNumber(t *testing.T) {
	for _, tc := range []struct {
		locale string
		v      int
		want   string
	}{
		{"en", 0, "0"},
		{"en", 7, "7"},
		{"en", 42, "42"},
		{"en", 999, "999"},
		{"en", -7, "-7"},
		{"en", 1234, "1,234"},
		{"en", 1234567, "1,234,567"},
		{"en-US", 1234567, "1,234,567"},
		{"de", 1234, "1.234"},
		{"de-DE", 1234567, "1.234.567"},
		{"it", 1234, "1.234"},
		{"tr", 1234, "1.234"},
		{"fr", 1234, "1\u202f234"},
		{"ru", 1234, "1\u00a0234"},
		{"pl", 1234567, "1\u00a0234\u00a0567"},
		// Hindi groups by two after the first three.
		{"hi", 1234, "1,234"},
		{"hi", 12345, "12,345"},
		{"hi", 1234567, "12,34,567"},
		{"hi", 123456789, "12,34,56,789"},
		// Arabic writes its own digits.
		{"ar", 7, "٧"},
		{"ar", 1234, "١٬٢٣٤"},
		{"ar-EG", 1234567, "١٬٢٣٤٬٥٦٧"},
		{"zh-Hans", 1234567, "1,234,567"},
		{"zh-Hant", 1234, "1,234"},
		{"ja", 1234567, "1,234,567"},
		// A language i18n has no data for is written as English.
		{"sv", 1234567, "1,234,567"},
		{"", 1234, "1,234"},
		{"sv-SE", 1234, "1,234"},
		// The POSIX spellings of the languages i18n knows.
		{"de_DE.UTF-8", 1234, "1.234"},
		{"zh_CN.UTF-8", 1234, "1,234"},
	} {
		if got := Number(tc.locale, tc.v); got != tc.want {
			t.Errorf("Number(%q, %d) = %q, want %q", tc.locale, tc.v, got, tc.want)
		}
	}
}

func TestNumberOfEveryType(t *testing.T) {
	for _, tc := range []struct {
		got  string
		want string
	}{
		{Number("en", int8(-8)), "-8"},
		{Number("en", int16(1234)), "1,234"},
		{Number("en", int32(1234567)), "1,234,567"},
		{Number("en", int64(-1234567)), "-1,234,567"},
		{Number("en", uint(1234)), "1,234"},
		{Number("en", uint8(8)), "8"},
		{Number("en", uint16(1234)), "1,234"},
		{Number("en", uint32(1234567)), "1,234,567"},
		{Number("en", uint64(4294967296)), "4,294,967,296"},
		{Number("en", float32(0.1)), "0.1"},
		{Number("en", 1234.5), "1,234.5"},
		{Number("en", -0.5), "-0.5"},
		{Number("de", 1234.5), "1.234,5"},
		{Number("de", -1234.5), "-1.234,5"},
		{Number("ar", 12.5), "١٢٫٥"},
		{Number("fr", 1234.5), "1\u202f234,5"},
		{Number("hi", 1234567.5), "12,34,567.5"},
		{Number("sv", 1234.5), "1,234.5"},
		// A type of its own, as an app often has.
		{Number("en", hundred(100)), "100"},
	} {
		if tc.got != tc.want {
			t.Errorf("Number(...) = %q, want %q", tc.got, tc.want)
		}
	}
}

type hundred int

func TestGroupDigits(t *testing.T) {
	for _, tc := range []struct {
		digits string
		sizes  []int
		want   string
	}{
		{"1", []int{3}, "1"},
		{"12", []int{3}, "12"},
		{"123", []int{3}, "123"},
		{"1234", []int{3}, "1 234"},
		{"1234567", []int{3}, "1 234 567"},
		{"1234567", []int{3, 2}, "12 34 567"},
		{"12345", []int{3, 2}, "12 345"},
		{"123456789", []int{3, 2}, "12 34 56 789"},
		{"1234", nil, "1234"},
		{"", []int{3}, ""},
	} {
		got := ""
		for i, group := range groupDigits(tc.digits, tc.sizes) {
			if i > 0 {
				got += " "
			}
			got += group
		}
		if got != tc.want {
			t.Errorf("groupDigits(%q, %v) = %q, want %q", tc.digits, tc.sizes, got, tc.want)
		}
	}
}
