package assistant

import (
	"regexp"
	"strings"
)

// fixTypos: the date and kind words people type in a hurry on a phone — «сегондя», «зафтра», «субота», «выхи».
// Rules and the model both read them right after this; a miss here means «что сегондя в кино» loses its day.
type typo struct {
	re *regexp.Regexp
	to string
}

// word: whole words only (Go's \b knows no Cyrillic), any case; the ending group is kept when there is one
func word(to string, alts ...string) typo {
	return typo{regexp.MustCompile(`(?i)(^|[^\p{L}])(?:` + strings.Join(alts, "|") + `)([^\p{L}]|$)`), "${1}" + to + "${2}"}
}

var typos = []typo{
	word("сегодня", "сегонд[яи]", "сегдня", "седня", "сёдня", "сеголня", "сегоня", "сеодня", "севодня", "сиводня", "сегодян", "сгодня"),
	word("завтра", "завтро", "зафтра", "заврта", "звтра", "завра"),
	word("субботу", "суботу"), word("субботы", "суботы"), word("суббота", "субота"), word("субботе", "суботе"),
	word("выходные", "выхи", "выходны", "выхныe"), word("выходных", "выхах"),
	word("кинотеатр", "кинотетр", "кинотеатор"),
	word("рестораны", "рестики", "рестиков", "рестик", "ресторанчики"),
}

func fixTypos(s string) string {
	for _, t := range typos {
		s = t.re.ReplaceAllString(s, t.to)
	}
	return s
}
