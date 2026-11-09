package domain

import (
	"testing"
	"time"
)

func TestBusy(t *testing.T) {
	msk, _ := time.LoadLocation("Europe/Moscow")
	ics := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nDTSTART;TZID=Europe/Moscow:20261110T100000\r\nDTEND;TZID=Europe/Moscow:20261110T120000\r\nSUMMARY:К врачу\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nDTSTART:20261109T060000Z\r\nDTEND:20261109T150000Z\r\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR\r\nSUMMARY:Работа\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nDTSTART;VALUE=DATE:20261114\r\nDTEND;VALUE=DATE:20261115\r\nSUMMARY:Дача\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nDTSTART:20261112T150000Z\r\nDURATION:PT3H\r\nTRANSP:TRANSPARENT\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	from := time.Date(2026, 11, 9, 0, 0, 0, 0, msk)
	spans := BusySpans(ics, from, from.AddDate(0, 0, 14), msk)
	slots := BusySlots(spans, from, 14, msk)
	for k, want := range map[string]bool{
		"2026-11-10:0": true,  // 10–12 of 9–13: half
		"2026-11-09:0": true,  // work 9–18 Mon
		"2026-11-09:1": true,  //
		"2026-11-09:2": false, // evening free
		"2026-11-11:1": true,  // Wed work
		"2026-11-12:1": false, // Thu, a «free» (transparent) event
		"2026-11-14:2": true,  // all day at the dacha
		"2026-11-16:0": true,  // the next Monday too
	} {
		if slots[k] != want {
			t.Errorf("%s busy=%v, want %v", k, slots[k], want)
		}
	}
}
