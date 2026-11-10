package app

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
)

func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

func urlq(s string) string { return url.QueryEscape(s) }

// NameIns: «с Настей», «с Вовой», «с Иваном» — simple Russian endings, anything else as is.
func NameIns(name string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) < 2 || !unicode.Is(unicode.Cyrillic, r[0]) || !unicode.IsUpper(r[0]) {
		return string(r)
	}
	for _, x := range r[1:] {
		if !unicode.Is(unicode.Cyrillic, x) || unicode.IsUpper(x) {
			return string(r)
		}
	}
	stem, last, pre := string(r[:len(r)-1]), r[len(r)-1], r[len(r)-2]
	hush := strings.ContainsRune("жшщчц", pre)
	switch {
	case last == 'я':
		return stem + "ей"
	case last == 'а' && hush:
		return stem + "ей"
	case last == 'а':
		return stem + "ой"
	case last == 'й' || last == 'ь':
		return stem + "ем"
	case strings.ContainsRune("жшщчц", last):
		return string(r) + "ем"
	case strings.ContainsRune("бвгдзклмнпрстфх", last):
		return string(r) + "ом"
	}
	return string(r)
}

// NameAcc: «позвал Настю», «позвала Вову»; anything unusual stays as is.
func NameAcc(name string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) < 2 || !unicode.Is(unicode.Cyrillic, r[0]) {
		return string(r)
	}
	switch last := r[len(r)-1]; {
	case last == 'я':
		return string(r[:len(r)-1]) + "ю"
	case last == 'а':
		return string(r[:len(r)-1]) + "у"
	case last == 'й' || last == 'ь':
		return string(r[:len(r)-1]) + "я"
	case strings.ContainsRune("бвгджзклмнпрстфхцчшщ", last):
		return string(r) + "а"
	}
	return string(r)
}

// Past: «отметила» for Настя, «отметил» for Вова — a past-tense verb agreed with the name. Names that are both
// (Саша, Женя) and unknown ones keep the neutral «(а)» only as the last resort.
func Past(name, verb string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) < 2 {
		return verb + "(а)"
	}
	switch strings.ToLower(string(r)) {
	case "саша", "женя", "валя", "сеня", "шура", "слава":
		return verb + "(а)"
	case "никита", "илья", "кузьма", "фома", "лука", "миша", "гоша", "дима", "вова", "коля", "толя", "петя", "ваня", "серёжа", "сережа", "лёша", "леша", "гриша", "костя", "юра", "боря", "витя", "федя", "стёпа", "степа", "вася", "тёма", "тема", "лёва", "лева", "паша", "даня", "саня", "рома", "гена", "жора", "лёня", "леня", "сева", "яша", "миля":
		return verb
	}
	if l := r[len(r)-1]; l == 'а' || l == 'я' {
		return verb + "а"
	}
	return verb
}
