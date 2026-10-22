// Package store defines the persistence boundary for propsies and dropsies and
// its concrete implementations. The Repository interface decouples the core
// logic from the backing store (SQLite locally, Firestore on GCP).
package store

import (
	"context"
	"errors"
	"time"
)

// Action is one propsy (Delta +1) or one dropsy (Delta -1) from one person to
// another.
type Action struct {
	From      string    `firestore:"from"`      // Slack user ID of the giver
	To        string    `firestore:"to"`        // Slack user ID of the receiver
	Reason    string    `firestore:"reason"`    // free text, may be empty
	ChannelID string    `firestore:"channelId"` // channel the command was run in
	Delta     int       `firestore:"delta"`     // +1 (propsy) or -1 (dropsy)
	CreatedAt time.Time `firestore:"createdAt"`
}

// Stats are a user's all-time totals.
type Stats struct {
	PropsiesReceived int
	DropsiesReceived int
	PropsiesGiven    int
	DropsiesGiven    int
}

// Score is propsies received minus dropsies received; it may be negative.
func (s Stats) Score() int { return s.PropsiesReceived - s.DropsiesReceived }

// ErrBadDelta is returned by AddActions when Action.Delta is not +1 or -1.
var ErrBadDelta = errors.New("action delta must be +1 or -1")

// Entry is one leaderboard row.
type Entry struct {
	UserID string
	Score  int
}

// Repository persists propsies and dropsies and serves the aggregates the app
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
	// Stats returns zero totals for an unknown user.
	Stats(ctx context.Context, userID string) (Stats, error)
	// Leaderboard returns up to limit users by score, highest first. It
	// includes everyone who has received a propsy or dropsy, so scores can be
	// zero or negative.
	Leaderboard(ctx context.Context, limit int) ([]Entry, error)
	// Recent returns up to limit propsies and dropsies received by userID,
	// newest first.
	Recent(ctx context.Context, userID string, limit int) ([]Action, error)
	Close() error
}

var (
	_ Repository = (*SQLite)(nil)
	_ Repository = (*Firestore)(nil)
)
