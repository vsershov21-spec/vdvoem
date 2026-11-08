package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// After «мы идём»: tickets or a table. The event's own link when there is one (the scout and channels bring
// ticket pages), otherwise a search that lands on sellers. «Билеты есть» tells the partner, so nobody buys twice.

// BookLink: where to buy / book, and the word for it ("" — nothing to buy: a walk, a free place)
func BookLink(i store.Idea) (label, link string) {
	free := i.PriceMin == 0 && (i.Category == "walk" || i.Category == "home")
	switch {
	case free:
		return "", ""
	case i.Category == "food":
		label = "Забронировать стол"
		if i.URL != "" {
			return label, i.URL
		}
		return label, "https://yandex.ru/maps/?text=" + url.QueryEscape(i.Title+" "+i.Address)
	default:
		label = "Купить билеты"
		if i.URL != "" {
			return label, i.URL
		}
		return label, "https://yandex.ru/search/?text=" + url.QueryEscape(i.Title+" билеты")
	}
}

func (a *App) bookButton(i store.Idea) *tg.Button {
	l, u := BookLink(i)
	if u == "" {
		return nil
	}
	b := tg.Link("🎟 "+l, u)
	return &b
}

func bookWord(i store.Idea) string { l, _ := BookLink(i); return l }

func otherName(c Couple, m store.Member) string {
	if m.ID == c.Me.ID {
		return c.PartnerName()
	}
	return c.Me.Name
}

// NameDat: «Насте», «Вове»
func NameDat(name string) string {
	r := []rune(name)
	if len(r) < 2 {
		return name
	}
	switch r[len(r)-1] {
	case 'а', 'я':
		return string(r[:len(r)-1]) + "е"
	case 'й', 'ь':
		return string(r[:len(r)-1]) + "ю"
	}
	if r[len(r)-1] >= 'б' && r[len(r)-1] <= 'щ' {
		return name + "у"
	}
	return name
}

// Tickets: who marked «билеты есть» for a meeting (0 — nobody yet)
func (a *App) Tickets(ctx context.Context, spaceID, roundID int64) int64 {
	id, _ := strconv.ParseInt(a.S.Setting(ctx, fmt.Sprintf("tickets:%d:%d", spaceID, roundID)), 10, 64)
	return id
}

// GotTickets: I have the tickets (or the table) — the partner hears it once
func (a *App) GotTickets(ctx context.Context, c Couple, roundID int64) error {
	r, err := a.round(ctx, c, roundID)
	if err != nil {
		return err
	}
	if r.State != domain.StatePlanned && r.State != domain.StateConfirm {
		return domain.ErrWrongState
	}
	if a.Tickets(ctx, c.Space.ID, r.ID) != 0 {
		return nil
	}
	if err := a.S.SetSetting(ctx, fmt.Sprintf("tickets:%d:%d", c.Space.ID, r.ID), strconv.FormatInt(c.Me.ID, 10)); err != nil {
		return err
	}
	i, _ := a.S.Idea(ctx, c.Space.ID, r.Chosen)
	if c.Partner != nil {
		what := "билеты"
		if i.Category == "food" {
			what = "стол"
		}
		a.send(ctx, c.Partner.ChatID, fmt.Sprintf("🎟 %s взял(а) %s на <b>%s</b>. Покупать не нужно.", esc(c.Me.Name), what, esc(i.Title)), nil)
	}
	a.Live.From(ctx, c.Space.ID)
	return nil
}
