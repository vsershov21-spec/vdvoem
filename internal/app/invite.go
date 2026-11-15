package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vdvoem/internal/domain"
	"vdvoem/internal/store"
	"vdvoem/internal/tg"
)

// The invitation of the new interface. The one who opens the link sees who calls and what two people do here,
// and the pair is made by «Принять приглашение» — never silently. A link that cannot be accepted says why
// and leaves one way on.

const (
	InviteOK      = "ok"
	InviteExpired = "expired" // three days have passed: «Попросить новую»
	InviteUsed    = "used"    // somebody has accepted it already: «Вова уже в паре»
	InvitePaired  = "paired"  // the one who opened it is in a pair
	InviteOwn     = "own"     // the author opened their own link
	InviteGone    = "gone"    // nothing is known about this link
)

var (
	ErrInviteExpired = errors.New("invite_expired")
	ErrInvitePaired  = errors.New("invite_paired")
	ErrInviteOwn     = errors.New("invite_own")
)

// InviteText: what the partner receives in Telegram next to the link
func InviteText(name string) string {
	return name + " зовёт тебя во «Вдвоём»: вместе выбирать, куда сходить, и делить дела"
}

type InviteView struct {
	State   string `json:"state"`
	From    string `json:"from"`              // who invites
	Partner string `json:"partner,omitempty"` // whom the reader is in a pair with
	Ideas   int    `json:"ideas"`             // the reader's own ideas that move into the pair with them
	Asked   bool   `json:"asked"`             // a new link has been asked for already
	V2      bool   `json:"-"`                 // the author has the new interface: the invited one gets its screen
	author  *store.Member
}

// InviteCode: the code of a link or of a /start payload («inv_ABC», «ABC»)
func InviteCode(s string) string {
	if k := strings.LastIndex(s, "inv_"); k >= 0 {
		s = s[k+4:]
	}
	return strings.TrimSpace(s)
}

// InviteV2: the author of this link has the new interface (false when nothing is known about the link)
func (a *App) InviteV2(ctx context.Context, code string) bool {
	inv, err := a.S.Invite(ctx, code)
	if err != nil {
		return false
	}
	ms, _ := a.S.Members(ctx, inv.SpaceID)
	for k, m := range ms {
		if m.ID == inv.AuthorID || (inv.AuthorID == 0 && k == 0) {
			return a.usesV2(ctx, m.ID)
		}
	}
	return false
}

// InviteInfo: the invitation as the one who opened it should see it
func (a *App) InviteInfo(ctx context.Context, c Couple, code string) InviteView {
	v := InviteView{State: InviteGone, Ideas: 0}
	inv, err := a.S.Invite(ctx, code)
	if err != nil {
		return v
	}
	ms, _ := a.S.Members(ctx, inv.SpaceID)
	for k := range ms {
		if ms[k].ID == inv.AuthorID || (v.author == nil && k == 0) {
			v.author = &ms[k]
		}
	}
	if v.author == nil {
		return v
	}
	v.From, v.Asked, v.V2 = v.author.Name, inv.Asked, a.usesV2(ctx, v.author.ID)
	switch {
	case inv.SpaceID == c.Space.ID && c.Partner == nil:
		v.State = InviteOwn
	case c.Partner != nil:
		v.State, v.Partner = InvitePaired, c.Partner.Name
	case inv.Used || len(ms) >= 2:
		v.State = InviteUsed
	case !inv.ExpiresAt.After(a.now()):
		v.State = InviteExpired
	default:
		v.State, v.Ideas = InviteOK, a.S.SoloIdeas(ctx, c.Space.ID)
	}
	return v
}

// AcceptInvite: «Принять приглашение». The one who was alone moves into the pair with everything they have;
// the author gets a line on the home screen and in the bot. Returns how many ideas came along.
func (a *App) AcceptInvite(ctx context.Context, c Couple, code string) (Couple, int, error) {
	v := a.InviteInfo(ctx, c, code)
	switch v.State {
	case InvitePaired:
		return c, 0, ErrInvitePaired
	case InviteOwn:
		return c, 0, ErrInviteOwn
	case InviteExpired:
		return c, 0, ErrInviteExpired
	case InviteOK:
	default:
		return c, 0, ErrInviteUsed
	}
	nc, joined, err := a.joinSpace(ctx, Profile{TGID: c.Me.TGUserID, ChatID: c.Me.ChatID, Name: c.Me.Name, Photo: c.Me.PhotoURL}, code, &c)
	if err != nil || !joined {
		return nc, 0, err
	}
	if nc.Partner != nil { // the row on the author's home screen and the same line in the bot: seen in one, gone in the other
		_, _ = a.Propose(ctx, nc, domain.ProposalNote, noteInvite, fmt.Sprintf("%s %s приглашение", nc.Me.Name, Past(nc.Me.Name, "принял")), "Вы в паре. Идеи, встречи и дела теперь общие", "")
	}
	a.Live.From(ctx, nc.Space.ID)
	return nc, v.Ideas, nil
}

// notes that are not about a setting have their own references, below zero (a setting's is its number)
const (
	noteInvite int64 = -1
	noteAsk    int64 = -2
)

// AskInvite: the link has run out, the invited one asks its author for a new one. The author hears it once.
func (a *App) AskInvite(ctx context.Context, c Couple, code string) error {
	v := a.InviteInfo(ctx, c, code)
	if v.State != InviteExpired || v.author == nil {
		return ErrInviteUsed
	}
	if !a.S.MarkInviteAsked(ctx, code, a.now()) {
		return nil // asked already
	}
	title := c.Me.Name + " просит новую ссылку"
	if _, err := a.S.CreateProposal(ctx, domain.Proposal{SpaceID: v.author.SpaceID, Kind: domain.ProposalNote, RefID: noteAsk, AuthorID: c.Me.ID,
		Title: title, Sub: "Прежняя устарела"}, a.now()); err != nil {
		return err
	}
	a.send(ctx, v.author.ChatID, fmt.Sprintf("<b>%s.</b> Прежняя устарела: приглашение живёт 3 дня.", esc(title)),
		tg.Keyboard(tg.Row(tg.CB("Новая ссылка", "inv"))))
	a.Live.From(ctx, v.author.SpaceID)
	return nil
}
