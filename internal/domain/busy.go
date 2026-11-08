package domain

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Busy time from a person's own calendar (a private iCal link: Google, iCloud, Yandex, Outlook). Only when —
// never what: titles are dropped while reading, so the partner sees «занят», not «к врачу».

type Span struct{ From, To time.Time }

// BusySpans: the busy intervals of an .ics between from and to (recurring weekly/daily events expanded)
func BusySpans(ics string, from, to time.Time, loc *time.Location) []Span {
	ics = strings.ReplaceAll(ics, "\r\n", "\n")
	ics = strings.ReplaceAll(ics, "\n ", "")
	ics = strings.ReplaceAll(ics, "\n\t", "")
	var out []Span
	for _, block := range strings.Split(ics, "BEGIN:VEVENT")[1:] {
		end := strings.Index(block, "END:VEVENT")
		if end < 0 {
			continue
		}
		ev := map[string]string{}
		params := map[string]string{}
		for _, line := range strings.Split(block[:end], "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			name, p, _ := strings.Cut(k, ";")
			name = strings.ToUpper(name)
			if _, seen := ev[name]; !seen {
				ev[name], params[name] = strings.TrimSpace(v), p
			}
		}
		if strings.EqualFold(ev["STATUS"], "CANCELLED") || strings.EqualFold(ev["TRANSP"], "TRANSPARENT") {
			continue
		}
		s, allDay, ok := icsTime(ev["DTSTART"], params["DTSTART"], loc)
		if !ok {
			continue
		}
		var e time.Time
		if v := ev["DTEND"]; v != "" {
			e, _, ok = icsTime(v, params["DTEND"], loc)
		} else if d := ev["DURATION"]; d != "" {
			e, ok = s.Add(isoDur(d)), true
		} else if allDay {
			e, ok = s.AddDate(0, 0, 1), true
		} else {
			e, ok = s.Add(time.Hour), true
		}
		if !ok || !e.After(s) {
			continue
		}
		for _, st := range repeat(s, ev["RRULE"], to, loc) {
			en := st.Add(e.Sub(s))
			if en.After(from) && st.Before(to) {
				out = append(out, Span{st, en})
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].From.Before(out[b].From) })
	return out
}

func icsTime(v, params string, loc *time.Location) (time.Time, bool, bool) {
	if v == "" {
		return time.Time{}, false, false
	}
	if strings.Contains(params, "VALUE=DATE") || len(v) == 8 {
		t, err := time.ParseInLocation("20060102", v[:8], loc)
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse("20060102T150405Z", v)
		return t, false, err == nil
	}
	l := loc
	for _, p := range strings.Split(params, ";") {
		if k, tz, ok := strings.Cut(p, "="); ok && strings.EqualFold(k, "TZID") {
			if z, err := time.LoadLocation(strings.Trim(tz, `"`)); err == nil {
				l = z
			}
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, l)
	return t, false, err == nil
}

// isoDur: PT1H30M, P1D
func isoDur(d string) time.Duration {
	var total time.Duration
	num := ""
	inT := false
	for _, r := range d {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inT = true
		case r == 'P':
		default:
			n, _ := strconv.Atoi(num)
			num = ""
			switch {
			case r == 'W':
				total += time.Duration(n) * 7 * 24 * time.Hour
			case r == 'D':
				total += time.Duration(n) * 24 * time.Hour
			case r == 'H' && inT:
				total += time.Duration(n) * time.Hour
			case r == 'M' && inT:
				total += time.Duration(n) * time.Minute
			}
		}
	}
	return total
}

var icsDays = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

// repeat: the starts of a (simple) recurring event up to «to»; DAILY and WEEKLY with BYDAY, INTERVAL, COUNT, UNTIL
func repeat(s time.Time, rule string, to time.Time, loc *time.Location) []time.Time {
	if rule == "" {
		return []time.Time{s}
	}
	r := map[string]string{}
	for _, p := range strings.Split(rule, ";") {
		if k, v, ok := strings.Cut(p, "="); ok {
			r[strings.ToUpper(k)] = v
		}
	}
	freq := r["FREQ"]
	if freq != "DAILY" && freq != "WEEKLY" {
		return []time.Time{s}
	}
	interval, _ := strconv.Atoi(r["INTERVAL"])
	if interval < 1 {
		interval = 1
	}
	count, _ := strconv.Atoi(r["COUNT"])
	until := to
	if u := r["UNTIL"]; u != "" {
		if t, _, ok := icsTime(strings.TrimSuffix(u, "Z")+map[bool]string{true: "Z", false: ""}[strings.HasSuffix(u, "Z")], "", loc); ok && t.Before(until) {
			until = t
		}
	}
	days := map[time.Weekday]bool{}
	for _, d := range strings.Split(r["BYDAY"], ",") {
		d = strings.TrimLeft(d, "+-0123456789")
		if wd, ok := icsDays[d]; ok {
			days[wd] = true
		}
	}
	var out []time.Time
	for day, n := 0, 0; ; day++ {
		t := s.AddDate(0, 0, day)
		if t.After(until) || count > 0 && n >= count || day > 800 {
			break
		}
		ok := false
		switch freq {
		case "DAILY":
			ok = day%interval == 0
		case "WEEKLY":
			week := day / 7
			if len(days) == 0 {
				ok = day%7 == 0 && week%interval == 0
			} else {
				ok = days[t.Weekday()] && week%interval == 0
			}
		}
		if ok {
			out = append(out, t)
			n++
		}
	}
	return out
}

// BusySlots: which parts of which days (YYYY-MM-DD:part) a person cannot do — busy for most of the part
func BusySlots(spans []Span, from time.Time, days int, loc *time.Location) map[string]bool {
	out := map[string]bool{}
	d0 := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	for d := 0; d < days; d++ {
		day := d0.AddDate(0, 0, d)
		for p, b := range PartBounds {
			ps, pe := day.Add(time.Duration(b[0])*time.Hour), day.Add(time.Duration(b[1])*time.Hour)
			var busy time.Duration
			for _, s := range spans {
				a, z := s.From, s.To
				if a.Before(ps) {
					a = ps
				}
				if z.After(pe) {
					z = pe
				}
				if z.After(a) {
					busy += z.Sub(a)
				}
			}
			if busy*2 >= pe.Sub(ps) { // half the part or more is taken
				out[day.Format("2006-01-02")+":"+strconv.Itoa(p)] = true
			}
		}
	}
	return out
}
