// Package store defines the persistence boundary for Propsies and Dropsies and
// its concrete implementations. The Repository interface decouples the core
// logic from the backing store (a JSON file locally, Firestore on GCP).
package store

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"
)

// Action is one Propsy (Delta +1) or one Dropsy (Delta -1) from one person to
// another.
type Action struct {
	From      string    `firestore:"from" json:"from"`           // Slack user ID of the giver
	To        string    `firestore:"to" json:"to"`               // Slack user ID of the receiver
	Reason    string    `firestore:"reason" json:"reason"`       // free text, may be empty
	ChannelID string    `firestore:"channelId" json:"channelId"` // channel the command was run in
	Delta     int       `firestore:"delta" json:"delta"`         // +1 (Propsy) or -1 (Dropsy)
	CreatedAt time.Time `firestore:"createdAt" json:"createdAt"`
}

// Stats are a user's all-time totals.
type Stats struct {
	PropsiesReceived int `json:"propsiesReceived"`
	DropsiesReceived int `json:"dropsiesReceived"`
	PropsiesGiven    int `json:"propsiesGiven"`
	DropsiesGiven    int `json:"dropsiesGiven"`
}

// Score is Propsies received minus Dropsies received; it may be negative.
func (s Stats) Score() int { return s.PropsiesReceived - s.DropsiesReceived }

// ErrBadDelta is returned by AddActions when Action.Delta is not +1 or -1.
var ErrBadDelta = errors.New("action delta must be +1 or -1")

// Board selects a leaderboard.
type Board int

// The leaderboards.
const (
	// BoardReceived ranks everyone who has received a Propsy or Dropsy by
	// score: Propsies received minus Dropsies received (so it can be zero or
	// negative).
	BoardReceived Board = iota + 1
	// BoardGiven ranks everyone who has given a Propsy or Dropsy by how many
	// they've given in total (Propsies and Dropsies both count).
	BoardGiven
)

// Entry is one leaderboard row; Count is the board's measure.
type Entry struct {
	UserID string
	Count  int
}

// topEntries returns up to limit entries of counts, highest first, ties broken
// by user ID so the order is stable. Both stores keep their boards as a map of
// user ID to count and rank them with this.
func topEntries(counts map[string]int, limit int) []Entry {
	out := make([]Entry, 0, len(counts))
	for id, n := range counts {
		out = append(out, Entry{UserID: id, Count: n})
	}
	slices.SortFunc(out, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.UserID, b.UserID))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Repository persists Propsies and Dropsies and serves the aggregates the app
// home and rate limits need.
type Repository interface {
	// AddActions records actions, all from the same giver, and updates the
	// users' totals, atomically and all-or-nothing. It enforces lim as of now
	// and returns a *LimitError, recording nothing, if a limit would be
	// exceeded.
	AddActions(ctx context.Context, actions []Action, lim Limits, now time.Time) error
	// Allowance reports how much of the overall limit userID has used in the
	// window open as of now, for a limit with the given period.
	Allowance(ctx context.Context, userID string, period time.Duration, now time.Time) (Allowance, error)
	// PairAllowance reports how much of the per-person limit from giver to
	// recipient is used in the window open as of now, for a limit with the given
	// period.
	PairAllowance(ctx context.Context, from, to string, period time.Duration, now time.Time) (Allowance, error)
	// Stats returns zero totals for an unknown user.
	Stats(ctx context.Context, userID string) (Stats, error)
	// Leaderboard returns up to limit entries of board, highest first and with
	// ties broken by user ID. Everyone the board covers is eligible: there's no
	// cut-off but limit.
	Leaderboard(ctx context.Context, board Board, limit int) ([]Entry, error)
	// Recent returns up to limit Propsies and Dropsies received by userID,
	// newest first.
	Recent(ctx context.Context, userID string, limit int) ([]Action, error)
	// Ping checks the store can be reached, for the readiness probe. It must be
	// cheap: it runs on every probe.
	Ping(ctx context.Context) error
	Close() error
}

var (
	_ Repository = (*JSONFile)(nil)
	_ Repository = (*Firestore)(nil)
)
