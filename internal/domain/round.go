package domain

import (
	"errors"
	"time"
)

// RoundState is where a «Давай куда-нибудь» round stands.
type RoundState string

const (
	StateTime      RoundState = "time"      // both mark when they are free
	StateVote      RoundState = "vote"      // three ideas, each votes
	StateConfirm   RoundState = "confirm"   // one idea chosen, both must confirm
	StatePlanned   RoundState = "planned"   // two confirmations: a meeting
	StateDone      RoundState = "done"      // «Сходили»
	StateExpired   RoundState = "expired"   // 48 h of silence: «Не сложилось»
	StateCancelled RoundState = "cancelled" // someone cancelled the planned meeting
)

func (s RoundState) Open() bool { return s == StateTime || s == StateVote || s == StateConfirm }

const (
	RoundTTL              = 48 * time.Hour
	VoteNone        int64 = 0
	VoteNoneOfThese int64 = -1
	DefaultDuration       = 120 // minutes when an idea doesn't say
)

type Round struct {
	ID        int64
	SpaceID   int64
	State     RoundState
	OpenedAt  time.Time
	ExpiresAt time.Time
	SlotsA    []Slot
	SlotsB    []Slot
	SentA     bool
	SentB     bool
	BudgetKop int64
	Triple    []int64
	Shown     []int64 // every idea shown in this round
	VoteA     int64
	VoteB     int64
	Chosen    int64
	ConfirmA  bool
	ConfirmB  bool
	StartsAt  time.Time
	EndsAt    time.Time
	Version   int
	Reminders int
	DoneA     string // "", "yes", "moved", "no"
	AgainA    string // "", "yes", "no"
	AgainB    string
}

var (
	ErrWrongState  = errors.New("round_wrong_state")
	ErrClosed      = errors.New("round_closed")
	ErrNotInTriple = errors.New("idea_not_in_triple")
)

func NewRound(spaceID int64, now time.Time, budget int64) Round {
	return Round{SpaceID: spaceID, State: StateTime, OpenedAt: now, ExpiresAt: now.Add(RoundTTL), BudgetKop: budget}
}

func (r *Round) bump() { r.Version++ }

// SubmitSlots stores one partner's free time. Returns true when both have answered.
func (r *Round) SubmitSlots(who string, slots []Slot, now time.Time) (bool, error) {
	if err := r.checkOpen(now); err != nil {
		return false, err
	}
	if r.State != StateTime {
		return false, ErrWrongState
	}
	if who == "a" {
		r.SlotsA, r.SentA = slots, true
	} else {
		r.SlotsB, r.SentB = slots, true
	}
	r.bump()
	return r.SentA && r.SentB, nil
}

// StartVoting moves to the vote with a picked triple. An empty triple keeps the round in StateTime.
func (r *Round) StartVoting(triple []int64) error {
	if r.State != StateTime || !(r.SentA && r.SentB) {
		return ErrWrongState
	}
	if len(triple) == 0 {
		return nil
	}
	r.State, r.Triple, r.VoteA, r.VoteB = StateVote, triple, VoteNone, VoteNone
	r.Shown = append(r.Shown, triple...)
	r.bump()
	return nil
}

// Vote records a vote. When both agree on one idea, the round moves to confirmation.
func (r *Round) Vote(who string, ideaID int64, now time.Time) (matched bool, err error) {
	if err := r.checkOpen(now); err != nil {
		return false, err
	}
	if r.State != StateVote {
		return false, ErrWrongState
	}
	if ideaID != VoteNoneOfThese && !contains(r.Triple, ideaID) {
		return false, ErrNotInTriple
	}
	if who == "a" {
		r.VoteA = ideaID
	} else {
		r.VoteB = ideaID
	}
	r.bump()
	if r.VoteA > 0 && r.VoteA == r.VoteB {
		r.State, r.Chosen = StateConfirm, r.VoteA
		r.ConfirmA, r.ConfirmB = false, false
		return true, nil
	}
	return false, nil
}

// VotedBoth tells whether both voted (possibly for different ideas).
func (r Round) VotedBoth() bool { return r.VoteA != VoteNone && r.VoteB != VoteNone }

// Reshuffle replaces the triple («Другая тройка»).
func (r *Round) Reshuffle(triple []int64, now time.Time) error {
	if err := r.checkOpen(now); err != nil {
		return err
	}
	if r.State != StateVote {
		return ErrWrongState
	}
	r.Triple, r.VoteA, r.VoteB = triple, VoteNone, VoteNone
	r.Shown = append(r.Shown, triple...)
	r.bump()
	return nil
}

// Choose skips the vote when both accept a compromise suggested by the bot.
func (r *Round) Choose(ideaID int64, now time.Time) error {
	if err := r.checkOpen(now); err != nil {
		return err
	}
	if r.State != StateVote {
		return ErrWrongState
	}
	r.State, r.Chosen, r.ConfirmA, r.ConfirmB = StateConfirm, ideaID, false, false
	r.bump()
	return nil
}

// SetTime fixes when the chosen idea happens (from the common window).
func (r *Round) SetTime(start time.Time, durationMin int) {
	if durationMin <= 0 {
		durationMin = DefaultDuration
	}
	r.StartsAt, r.EndsAt = start, start.Add(time.Duration(durationMin)*time.Minute)
	r.bump()
}

// Confirm: silence is never consent; only two explicit confirmations plan the meeting.
func (r *Round) Confirm(who string, now time.Time) (planned bool, err error) {
	if err := r.checkOpen(now); err != nil {
		return false, err
	}
	if r.State != StateConfirm {
		return false, ErrWrongState
	}
	if who == "a" {
		r.ConfirmA = true
	} else {
		r.ConfirmB = true
	}
	r.bump()
	if r.ConfirmA && r.ConfirmB {
		r.State = StatePlanned
		return true, nil
	}
	return false, nil
}

// OtherTime sends the round back to picking time but keeps the chosen idea.
func (r *Round) OtherTime(now time.Time) error {
	if r.State != StateConfirm && r.State != StatePlanned {
		return ErrWrongState
	}
	r.State = StateTime
	r.SlotsA, r.SlotsB, r.SentA, r.SentB = nil, nil, false, false
	r.ConfirmA, r.ConfirmB = false, false
	r.ExpiresAt = now.Add(RoundTTL)
	r.bump()
	return nil
}

func (r *Round) Cancel() error {
	if r.State != StatePlanned && !r.State.Open() {
		return ErrWrongState
	}
	r.State = StateCancelled
	r.bump()
	return nil
}

func (r *Round) MarkDone(who, answer string) error {
	if r.State != StatePlanned && r.State != StateDone {
		return ErrWrongState
	}
	if answer == "yes" {
		r.State = StateDone
	}
	r.DoneA = answer
	r.bump()
	return nil
}

func (r *Round) SetAgain(who, answer string) error {
	if r.State != StateDone {
		return ErrWrongState
	}
	if who == "a" {
		r.AgainA = answer
	} else {
		r.AgainB = answer
	}
	r.bump()
	return nil
}

// Tick expires a silent round. Returns true when it expired now.
func (r *Round) Tick(now time.Time) bool {
	if r.State.Open() && !now.Before(r.ExpiresAt) {
		r.State = StateExpired
		r.bump()
		return true
	}
	return false
}

// Waiting returns who still has to act in the current state ("a", "b" or both).
func (r Round) Waiting() []string {
	var w []string
	switch r.State {
	case StateTime:
		if !r.SentA {
			w = append(w, "a")
		}
		if !r.SentB {
			w = append(w, "b")
		}
	case StateVote:
		if r.VoteA == VoteNone {
			w = append(w, "a")
		}
		if r.VoteB == VoteNone {
			w = append(w, "b")
		}
	case StateConfirm:
		if !r.ConfirmA {
			w = append(w, "a")
		}
		if !r.ConfirmB {
			w = append(w, "b")
		}
	}
	return w
}

// ReminderDue: a nudge at 12 h and 36 h after opening, never more than two.
func (r Round) ReminderDue(now time.Time) bool {
	if !r.State.Open() || len(r.Waiting()) == 0 {
		return false
	}
	el := now.Sub(r.OpenedAt)
	return (r.Reminders == 0 && el >= 12*time.Hour) || (r.Reminders == 1 && el >= 36*time.Hour)
}

func (r *Round) checkOpen(now time.Time) error {
	r.Tick(now)
	if !r.State.Open() {
		return ErrClosed
	}
	return nil
}

func contains(xs []int64, x int64) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
