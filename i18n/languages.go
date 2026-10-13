package i18n

import "slices"

// lang is what i18n knows of a language: the plural forms it has, how it
// writes numbers, and how it writes dates.
type lang struct {
	plural      func(n int) Plural
	decimal     string
	group       string
	groupSizes  []int
	digits      string
	monthsShort [12]string
	monthsLong  [12]string
	weekdays    [7]string
	dateShort   string
	dateMedium  string
	dateLong    string
	time        string
	am, pm      string
}

// Languages returns the tags of the languages i18n has its own data for,
// sorted: their plural forms, their numbers and their dates. A language of
// another tag is written as English numbers, ISO dates and the plural form
// Other.
func Languages() []string {
	tags := make([]string, 0, len(langs))
	for tag := range langs {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	return tags
}

// languageOf returns the data of the language whose tag suits locale, and
// whether i18n has any: "zh-Hant-HK" is "zh-Hant", "pt-BR" is "pt" and
// "de_DE.UTF-8" is "de".
func languageOf(locale string) (lang, bool) {
	language, script, region := split(locale)
	if language == "" {
		return lang{}, false
	}
	candidates := make([]string, 0, 3)
	if script != "" {
		candidates = append(candidates, language+"-"+script)
	}
	if region != "" {
		candidates = append(candidates, language+"-"+region)
	}
	candidates = append(candidates, language)
	for _, tag := range candidates {
		if l, ok := langs[tag]; ok {
			return l, true
		}
	}
	return lang{}, false
}

// langs holds the data of every language i18n knows. The months of a
// language are the ones a date uses, which some languages write differently
// from the month alone: Russian and Polish write the genitive, as in
// "5 марта 2026". A language that writes the same names for the short and
// the long month repeats them.
var langs = map[string]lang{
	"ar": {
		plural:     pluralArabic,
		decimal:    "\u066b",
		group:      "\u066c",
		groupSizes: []int{3},
		digits:     "٠١٢٣٤٥٦٧٨٩",
		monthsShort: [12]string{
			"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو",
			"يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
		},
		monthsLong: [12]string{
			"يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو",
			"يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر",
		},
		weekdays:   [7]string{"الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"},
		dateShort:  "d/M/y",
		dateMedium: "dd/MM/y",
		dateLong:   "EEEE، d MMMM y",
		time:       "h:mm a",
		am:         "ص",
		pm:         "م",
	},
	"de": {
		plural:     pluralOneOther,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni",
			"Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez.",
		},
		monthsLong: [12]string{
			"Januar", "Februar", "März", "April", "Mai", "Juni",
			"Juli", "August", "September", "Oktober", "November", "Dezember",
		},
		weekdays:   [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
		dateShort:  "dd.MM.yy",
		dateMedium: "dd.MM.y",
		dateLong:   "EEEE, d. MMMM y",
		time:       "HH:mm",
	},
	"en": {
		plural:     pluralOneOther,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"Jan", "Feb", "Mar", "Apr", "May", "Jun",
			"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
		},
		monthsLong: [12]string{
			"January", "February", "March", "April", "May", "June",
			"July", "August", "September", "October", "November", "December",
		},
		weekdays:   [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
		dateShort:  "M/d/yy",
		dateMedium: "MMM d, y",
		dateLong:   "EEEE, MMMM d, y",
		time:       "h:mm a",
		am:         "AM",
		pm:         "PM",
	},
	"es": {
		plural:     pluralOneOther,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"ene", "feb", "mar", "abr", "may", "jun",
			"jul", "ago", "sept", "oct", "nov", "dic",
		},
		monthsLong: [12]string{
			"enero", "febrero", "marzo", "abril", "mayo", "junio",
			"julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
		},
		weekdays:   [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		dateShort:  "d/M/yy",
		dateMedium: "d MMM y",
		dateLong:   "EEEE, d 'de' MMMM 'de' y",
		time:       "H:mm",
	},
	"fr": {
		plural:     pluralZeroOne,
		decimal:    ",",
		group:      "\u202f",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"janv.", "févr.", "mars", "avr.", "mai", "juin",
			"juil.", "août", "sept.", "oct.", "nov.", "déc.",
		},
		monthsLong: [12]string{
			"janvier", "février", "mars", "avril", "mai", "juin",
			"juillet", "août", "septembre", "octobre", "novembre", "décembre",
		},
		weekdays:   [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
		dateShort:  "dd/MM/y",
		dateMedium: "d MMM y",
		dateLong:   "EEEE d MMMM y",
		time:       "HH:mm",
	},
	"he": {
		plural:     pluralHebrew,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"ינואר", "פברואר", "מרץ", "אפריל", "מאי", "יוני",
			"יולי", "אוגוסט", "ספטמבר", "אוקטובר", "נובמבר", "דצמבר",
		},
		monthsLong: [12]string{
			"ינואר", "פברואר", "מרץ", "אפריל", "מאי", "יוני",
			"יולי", "אוגוסט", "ספטמבר", "אוקטובר", "נובמבר", "דצמבר",
		},
		weekdays:   [7]string{"יום ראשון", "יום שני", "יום שלישי", "יום רביעי", "יום חמישי", "יום שישי", "יום שבת"},
		dateShort:  "d.M.y",
		dateMedium: "d בMMM y",
		dateLong:   "EEEE, d בMMMM y",
		time:       "H:mm",
	},
	"hi": {
		plural:     pluralHindi,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3, 2},
		monthsShort: [12]string{
			"जन॰", "फ़र॰", "मार्च", "अप्रैल", "मई", "जून",
			"जुलाई", "अग॰", "सित॰", "अक्तू॰", "नव॰", "दिस॰",
		},
		monthsLong: [12]string{
			"जनवरी", "फ़रवरी", "मार्च", "अप्रैल", "मई", "जून",
			"जुलाई", "अगस्त", "सितंबर", "अक्तूबर", "नवंबर", "दिसंबर",
		},
		weekdays:   [7]string{"रविवार", "सोमवार", "मंगलवार", "बुधवार", "गुरुवार", "शुक्रवार", "शनिवार"},
		dateShort:  "d/M/yy",
		dateMedium: "d MMM y",
		dateLong:   "EEEE, d MMMM y",
		time:       "h:mm a",
		am:         "am",
		pm:         "pm",
	},
	"it": {
		plural:     pluralOneOther,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"gen", "feb", "mar", "apr", "mag", "giu",
			"lug", "ago", "set", "ott", "nov", "dic",
		},
		monthsLong: [12]string{
			"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
			"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre",
		},
		weekdays:   [7]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"},
		dateShort:  "dd/MM/yy",
		dateMedium: "d MMM y",
		dateLong:   "EEEE d MMMM y",
		time:       "HH:mm",
	},
	"ja": {
		plural:     pluralOnlyOther,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		monthsLong: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		weekdays:   [7]string{"日曜日", "月曜日", "火曜日", "水曜日", "木曜日", "金曜日", "土曜日"},
		dateShort:  "y/MM/dd",
		dateMedium: "y年M月d日",
		dateLong:   "y年M月d日EEEE",
		time:       "H:mm",
	},
	"ko": {
		plural:     pluralOnlyOther,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"1월", "2월", "3월", "4월", "5월", "6월",
			"7월", "8월", "9월", "10월", "11월", "12월",
		},
		monthsLong: [12]string{
			"1월", "2월", "3월", "4월", "5월", "6월",
			"7월", "8월", "9월", "10월", "11월", "12월",
		},
		weekdays:   [7]string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"},
		dateShort:  "yy. M. d.",
		dateMedium: "y. M. d.",
		dateLong:   "y년 M월 d일 EEEE",
		time:       "a h:mm",
		am:         "오전",
		pm:         "오후",
	},
	"nl": {
		plural:     pluralOneOther,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"jan.", "feb.", "mrt.", "apr.", "mei", "jun.",
			"jul.", "aug.", "sep.", "okt.", "nov.", "dec.",
		},
		monthsLong: [12]string{
			"januari", "februari", "maart", "april", "mei", "juni",
			"juli", "augustus", "september", "oktober", "november", "december",
		},
		weekdays:   [7]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"},
		dateShort:  "dd-MM-y",
		dateMedium: "d MMM y",
		dateLong:   "EEEE d MMMM y",
		time:       "HH:mm",
	},
	"pl": {
		plural:     pluralPolish,
		decimal:    ",",
		group:      "\u00a0",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"sty", "lut", "mar", "kwi", "maj", "cze",
			"lip", "sie", "wrz", "paź", "lis", "gru",
		},
		monthsLong: [12]string{
			"stycznia", "lutego", "marca", "kwietnia", "maja", "czerwca",
			"lipca", "sierpnia", "września", "października", "listopada", "grudnia",
		},
		weekdays:   [7]string{"niedziela", "poniedziałek", "wtorek", "środa", "czwartek", "piątek", "sobota"},
		dateShort:  "dd.MM.y",
		dateMedium: "d MMM y",
		dateLong:   "EEEE, d MMMM y",
		time:       "HH:mm",
	},
	"pt": {
		plural:     pluralZeroOne,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"jan.", "fev.", "mar.", "abr.", "mai.", "jun.",
			"jul.", "ago.", "set.", "out.", "nov.", "dez.",
		},
		monthsLong: [12]string{
			"janeiro", "fevereiro", "março", "abril", "maio", "junho",
			"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
		},
		weekdays:   [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
		dateShort:  "dd/MM/y",
		dateMedium: "d MMM y",
		dateLong:   "EEEE, d 'de' MMMM 'de' y",
		time:       "HH:mm",
	},
	"ru": {
		plural:     pluralRussian,
		decimal:    ",",
		group:      "\u00a0",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"янв.", "февр.", "мар.", "апр.", "мая", "июн.",
			"июл.", "авг.", "сент.", "окт.", "нояб.", "дек.",
		},
		monthsLong: [12]string{
			"января", "февраля", "марта", "апреля", "мая", "июня",
			"июля", "августа", "сентября", "октября", "ноября", "декабря",
		},
		weekdays:   [7]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
		dateShort:  "dd.MM.y",
		dateMedium: "d MMM y 'г'.",
		dateLong:   "EEEE, d MMMM y 'г'.",
		time:       "HH:mm",
	},
	"tr": {
		plural:     pluralOneOther,
		decimal:    ",",
		group:      ".",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"Oca", "Şub", "Mar", "Nis", "May", "Haz",
			"Tem", "Ağu", "Eyl", "Eki", "Kas", "Ara",
		},
		monthsLong: [12]string{
			"Ocak", "Şubat", "Mart", "Nisan", "Mayıs", "Haziran",
			"Temmuz", "Ağustos", "Eylül", "Ekim", "Kasım", "Aralık",
		},
		weekdays:   [7]string{"Pazar", "Pazartesi", "Salı", "Çarşamba", "Perşembe", "Cuma", "Cumartesi"},
		dateShort:  "d.MM.y",
		dateMedium: "d MMM y",
		dateLong:   "d MMMM y EEEE",
		time:       "HH:mm",
	},
	"zh-Hans": {
		plural:     pluralOnlyOther,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		monthsLong: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		weekdays:   [7]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"},
		dateShort:  "y/M/d",
		dateMedium: "y年M月d日",
		dateLong:   "y年M月d日EEEE",
		time:       "ah:mm",
		am:         "上午",
		pm:         "下午",
	},
	"zh-Hant": {
		plural:     pluralOnlyOther,
		decimal:    ".",
		group:      ",",
		groupSizes: []int{3},
		monthsShort: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		monthsLong: [12]string{
			"1月", "2月", "3月", "4月", "5月", "6月",
			"7月", "8月", "9月", "10月", "11月", "12月",
		},
		weekdays:   [7]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"},
		dateShort:  "y/M/d",
		dateMedium: "y年M月d日",
		dateLong:   "y年M月d日EEEE",
		time:       "ah:mm",
		am:         "上午",
		pm:         "下午",
	},
}
