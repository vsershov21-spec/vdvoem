package domain

import (
	"fmt"
	"strings"
	"time"
)

// QuietShift moves a send time out of quiet hours (23:00–09:00 local) to 09:00.
func QuietShift(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	h := l.Hour()
	if h >= 23 {
		n := l.AddDate(0, 0, 1)
		return time.Date(n.Year(), n.Month(), n.Day(), 9, 0, 0, 0, loc)
	}
	if h < 9 {
		return time.Date(l.Year(), l.Month(), l.Day(), 9, 0, 0, 0, loc)
	}
	return t
}

// ICS renders one VEVENT (RFC 5545). The same UID with a higher sequence updates the event.
type ICS struct {
	UID       string
	Sequence  int
	Title     string
	Location  string
	URL       string
	Desc      string
	Start     time.Time
	End       time.Time
	Stamp     time.Time
	Cancelled bool
	More      []ICS // next stops of the same meeting (museum → dinner), one VEVENT each
}

func icsEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`)
	return r.Replace(s)
}

// fold breaks lines longer than 75 octets as the RFC requires.
func fold(line string) string {
	b := []byte(line)
	if len(b) <= 75 {
		return line
	}
	var out strings.Builder
	cur := 0
	for len(b) > 0 {
		lim := 75
		if cur > 0 {
			lim = 74
		}
		n := lim
		if n > len(b) {
			n = len(b)
		}
		for n > 0 && n < len(b) && b[n]&0xC0 == 0x80 { // don't split a UTF-8 rune
			n--
		}
		if cur > 0 {
			out.WriteString("\r\n ")
		}
		out.Write(b[:n])
		b = b[n:]
		cur++
	}
	return out.String()
}

func (e ICS) Render() string {
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Vdvoem//RU", "CALSCALE:GREGORIAN",
	}
	if e.Cancelled {
		lines = append(lines, "METHOD:CANCEL")
	} else {
		lines = append(lines, "METHOD:PUBLISH")
	}
	lines = append(lines, e.vevent(true)...)
	for _, m := range e.More {
		m.Cancelled, m.Stamp = e.Cancelled, e.Stamp
		lines = append(lines, m.vevent(false)...)
	}
	lines = append(lines, "END:VCALENDAR")
	for i, l := range lines {
		lines[i] = fold(l)
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}

func (e ICS) vevent(alarm bool) []string {
	ts := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	lines := []string{"BEGIN:VEVENT",
		"UID:" + e.UID,
		fmt.Sprintf("SEQUENCE:%d", e.Sequence),
		"DTSTAMP:" + ts(e.Stamp),
		"DTSTART:" + ts(e.Start),
		"DTEND:" + ts(e.End),
		"SUMMARY:" + icsEscape(e.Title)}
	if e.Location != "" {
		lines = append(lines, "LOCATION:"+icsEscape(e.Location))
	}
	if e.Desc != "" {
		lines = append(lines, "DESCRIPTION:"+icsEscape(e.Desc))
	}
	if e.URL != "" {
		lines = append(lines, "URL:"+e.URL)
	}
	if e.Cancelled {
		lines = append(lines, "STATUS:CANCELLED")
	} else {
		lines = append(lines, "STATUS:CONFIRMED")
		if alarm {
			lines = append(lines, "BEGIN:VALARM", "TRIGGER:-PT2H", "ACTION:DISPLAY", "DESCRIPTION:"+icsEscape(e.Title), "END:VALARM")
		}
	}
	return append(lines, "END:VEVENT")
}

// Money formats kopecks as «4 000 ₽» with a narrow no-break space.
func Money(kop int64) string {
	r := kop / 100
	s := fmt.Sprintf("%d", r)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, []byte(" ")...)
		}
		out = append(out, c)
	}
	return string(out) + " ₽"
}

var ruWeek = [...]string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
var ruMon = [...]string{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// When formats «Сб, 15 ноя · 19:30».
func When(t time.Time, loc *time.Location) string {
	l := t.In(loc)
	return fmt.Sprintf("%s, %d %s · %02d:%02d", ruWeek[l.Weekday()], l.Day(), ruMon[l.Month()-1], l.Hour(), l.Minute())
}
