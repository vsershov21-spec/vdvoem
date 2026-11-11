package assistant

import (
	"strings"
	"time"
)

// Periods in plain words — «в конце ноября», «в начале декабря», «на следующей неделе», «до конца месяца»,
// «в декабре» — even with a typo in the month («наебря»: a voice message or a fast thumb). The answer then
// shows only what is on in those days.

var monthStems = []string{"январ", "феврал", "март", "апрел", "ма", "июн", "июл", "август", "сентябр", "октябр", "ноябр", "декабр"}
var monthForms = [][]string{{"январь", "января", "январе"}, {"февраль", "февраля", "феврале"}, {"март", "марта", "марте"}, {"апрель", "апреля", "апреле"},
	{"май", "мая", "мае"}, {"июнь", "июня", "июне"}, {"июль", "июля", "июле"}, {"август", "августа", "августе"}, {"сентябрь", "сентября", "сентябре"},
	{"октябрь", "октября", "октябре"}, {"ноябрь", "ноября", "ноябре"}, {"декабрь", "декабря", "декабре"}}

// monthOf: the month a word names, forgiving one or two wrong letters in a long word
func monthOf(w string) int {
	w = strings.Trim(w, "?!.,«»\"")
	r := []rune(w)
	if len(r) < 3 {
		return 0
	}
	for m, forms := range monthForms {
		for _, f := range forms {
			if w == f {
				return m + 1
			}
			if len(r) >= 5 && len([]rune(f)) >= 5 && lev(w, f) <= 2 && r[0] == []rune(f)[0] {
				return m + 1
			}
		}
	}
	return 0
}

func lev(a, b string) int {
	x, y := []rune(a), []rune(b)
	prev := make([]int, len(y)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(x); i++ {
		cur := make([]int, len(y)+1)
		cur[0] = i
		for j := 1; j <= len(y); j++ {
			c := 1
			if x[i-1] == y[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(y)]
}

// periodFrom: the days a request is about ("" — none named)
func periodFrom(text string, now time.Time) (from, to string) {
	t := strings.ToLower(text)
	day := func(d time.Time) string { return d.Format("2006-01-02") }
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	clip := func(a, b time.Time) (string, string) {
		if a.Before(today) {
			a = today
		}
		if b.Before(a) {
			return "", ""
		}
		return day(a), day(b)
	}
	monthEnd := func(y int, m time.Month) time.Time { return time.Date(y, m+1, 0, 0, 0, 0, 0, now.Location()) }
	switch {
	case has(t, "следующей неделе", "следующую неделю", "след неделе", "через неделю"):
		wd := (int(today.Weekday()) + 6) % 7 // Monday = 0
		mon := today.AddDate(0, 0, 7-wd)
		return clip(mon, mon.AddDate(0, 0, 6))
	case has(t, "до конца месяца", "в этом месяце", "этом месяце"):
		return clip(today, monthEnd(today.Year(), today.Month()))
	case has(t, "в следующем месяце", "следующем месяце"):
		n := time.Date(today.Year(), today.Month()+1, 1, 0, 0, 0, 0, now.Location())
		return clip(n, monthEnd(n.Year(), n.Month()))
	case has(t, "ближайшие две недели", "ближайшие 2 недели", "две недели"):
		return clip(today, today.AddDate(0, 0, 13))
	case has(t, "новый год", "новогод", "нового года", "на нг", "в нг", "новом году") && !has(t, "после нового года", "после нг"):
		// the eve and the holidays after it: 30 December — 8 January
		y := today.Year()
		if today.Month() == time.January && today.Day() > 8 || today.Month() > time.January {
			// this year's December ahead
		} else {
			y-- // early January: the holidays now
		}
		return clip(time.Date(y, time.December, 30, 0, 0, 0, 0, now.Location()), time.Date(y+1, time.January, 8, 0, 0, 0, 0, now.Location()))
	}
	for _, w := range strings.Fields(t) {
		m := monthOf(w)
		if m == 0 {
			continue
		}
		y := today.Year()
		if time.Month(m) < today.Month() {
			y++
		}
		first := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, now.Location())
		last := monthEnd(y, time.Month(m))
		switch {
		case has(t, "в начале", "начало", "первой половине"):
			return clip(first, first.AddDate(0, 0, 9))
		case has(t, "в середине", "середина"):
			return clip(first.AddDate(0, 0, 9), first.AddDate(0, 0, 19))
		case has(t, "в конце", "конец", "под конец", "второй половине"):
			return clip(first.AddDate(0, 0, 19), last)
		}
		return clip(first, last)
	}
	return "", ""
}
