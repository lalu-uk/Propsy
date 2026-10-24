package store

import (
	"fmt"
	"time"
)

// Limits are the rate limits AddActions enforces. A limit of 0 is off.
//
// Allowances are fixed windows, not rolling ones: a giver's window opens when
// they first give something, the count only goes up while it's open, and once
// the period has elapsed the allowance resets to full in one go (the next
// action opens a fresh window). Overall is per giver; PerPerson is per
// (giver, recipient) pair.
type Limits struct {
	Overall         int
	OverallPeriod   time.Duration
	PerPerson       int
	PerPersonPeriod time.Duration
}

// Allowance is how much of the overall limit a giver has used in their
// current window.
type Allowance struct {
	Used     int
	ResetsAt time.Time // zero when Used == 0: no window is open
}

// Window is one open allowance window. The zero value means none is open.
type Window struct {
	Start time.Time `json:"start"`
	Used  int       `json:"used"`
}

// live returns w, or the zero Window if it has expired as of now.
func (w Window) live(now time.Time, period time.Duration) Window {
	if w.Used == 0 || !now.Before(w.Start.Add(period)) {
		return Window{}
	}
	return w
}

func (w Window) resetsAt(period time.Duration) time.Time {
	if w.Used == 0 {
		return time.Time{}
	}
	return w.Start.Add(period)
}

// add spends n, opening the window at now if it isn't open.
func (w Window) add(n int, now time.Time) Window {
	if w.Used == 0 {
		w.Start = now
	}
	w.Used += n
	return w
}

// LimitKind says which limit an action would have exceeded.
type LimitKind int

// The kinds of limit.
const (
	LimitOverall LimitKind = iota + 1
	LimitPerPerson
)

// Capped is a recipient who has hit the per-person limit.
type Capped struct {
	UserID   string
	ResetsAt time.Time
}

// LimitError is returned by AddActions, with nothing recorded, when a limit
// would be exceeded.
type LimitError struct {
	Kind   LimitKind
	Limit  int
	Period time.Duration

	// LimitOverall: how many are left, and when the allowance resets.
	Left     int
	ResetsAt time.Time

	// LimitPerPerson: who's capped, and when each of their limits resets.
	Capped []Capped
}

func (e *LimitError) Error() string {
	if e.Kind == LimitPerPerson {
		return fmt.Sprintf("per-person limit of %d per %s reached", e.Limit, e.Period)
	}
	return fmt.Sprintf("limit of %d per %s reached", e.Limit, e.Period)
}

// recipients returns the distinct recipients of actions, in order, with how
// many actions each gets.
func recipients(actions []Action) (order []string, counts map[string]int) {
	counts = map[string]int{}
	for _, a := range actions {
		if counts[a.To] == 0 {
			order = append(order, a.To)
		}
		counts[a.To]++
	}
	return order, counts
}

// plan applies lim to actions given the giver's current windows, returning the
// windows to persist, or a *LimitError. It is pure so both stores share it;
// each store just loads the windows and saves the result in one transaction.
// pairs may omit recipients who have no window.
func plan(lim Limits, now time.Time, actions []Action, overall Window, pairs map[string]Window) (Window, map[string]Window, *LimitError) {
	order, counts := recipients(actions)

	if lim.Overall > 0 {
		overall = overall.live(now, lim.OverallPeriod)
		if left := lim.Overall - overall.Used; left < len(actions) {
			return Window{}, nil, &LimitError{
				Kind: LimitOverall, Limit: lim.Overall, Period: lim.OverallPeriod,
				Left: max(left, 0), ResetsAt: overall.resetsAt(lim.OverallPeriod),
			}
		}
		overall = overall.add(len(actions), now)
	}

	updated := map[string]Window{}
	if lim.PerPerson > 0 {
		var capped []Capped
		for _, to := range order {
			w := pairs[to].live(now, lim.PerPersonPeriod)
			if w.Used+counts[to] > lim.PerPerson {
				capped = append(capped, Capped{UserID: to, ResetsAt: w.resetsAt(lim.PerPersonPeriod)})
				continue
			}
			updated[to] = w.add(counts[to], now)
		}
		if len(capped) > 0 {
			return Window{}, nil, &LimitError{Kind: LimitPerPerson, Limit: lim.PerPerson, Period: lim.PerPersonPeriod, Capped: capped}
		}
	}
	return overall, updated, nil
}
