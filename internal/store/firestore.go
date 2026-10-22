package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Layout:
//   - users/{id} holds counters: propsiesReceived, dropsiesReceived,
//     propsiesGiven, dropsiesGiven, and score (propsies minus dropsies
//     received), plus the giver's overall rate-limit window: windowStart,
//     windowUsed.
//   - users/{id}/received/{auto} holds each propsy or dropsy that user received.
//   - users/{from}/pairs/{to} holds the per-person rate-limit window
//     (windowStart, windowUsed) for from giving to.
//
// Rate limits are fixed windows held as a counter and a start time (see
// Limits), checked and updated in the same transaction as the action, so
// they're enforced atomically and cost O(1) reads. The leaderboard is a single
// indexed query on score (no scans), and every query orders on one field only,
// so just automatic single-field indexes are needed: no composite index to
// provision.
const (
	usersCollection    = "users"
	receivedCollection = "received"
	pairsCollection    = "pairs"
)

// Firestore is a Repository backed by Google Cloud Firestore. Firestore
// bills per operation and storage, so an idle instance costs ~nothing.
type Firestore struct {
	client *firestore.Client
}

// NewFirestore opens a client for projectID/databaseID ("(default)" for the
// default database). FIRESTORE_EMULATOR_HOST, if set, redirects to the emulator.
func NewFirestore(ctx context.Context, projectID, databaseID string) (*Firestore, error) {
	client, err := firestore.NewClientWithDatabase(ctx, projectID, databaseID)
	if err != nil {
		return nil, fmt.Errorf("open firestore client (project %q, database %q): %w", projectID, databaseID, err)
	}
	return &Firestore{client: client}, nil
}

func (f *Firestore) user(id string) *firestore.DocumentRef {
	return f.client.Collection(usersCollection).Doc(id)
}

// windowDoc is the rate-limit window as stored on a user or pair document.
type windowDoc struct {
	Start time.Time `firestore:"windowStart"`
	Used  int       `firestore:"windowUsed"`
}

func (d windowDoc) window() Window { return Window(d) }

// readWindow loads the window on ref within tx; a missing document or one
// with no window yet is the zero Window.
func readWindow(tx *firestore.Transaction, ref *firestore.DocumentRef) (Window, error) {
	snap, err := tx.Get(ref)
	if status.Code(err) == codes.NotFound {
		return Window{}, nil
	}
	if err != nil {
		return Window{}, err
	}
	var d windowDoc
	if err := snap.DataTo(&d); err != nil {
		return Window{}, err
	}
	return d.window(), nil
}

// AddActions implements Repository.
func (f *Firestore) AddActions(ctx context.Context, actions []Action, lim Limits, now time.Time) error {
	if len(actions) == 0 {
		return nil
	}
	for _, a := range actions {
		if a.Delta != 1 && a.Delta != -1 {
			return ErrBadDelta
		}
	}
	from := f.user(actions[0].From)
	order, _ := recipients(actions)

	err := f.client.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		// All reads first: Firestore transactions require it.
		var overall Window
		pairs := map[string]Window{}
		var err error
		if lim.Overall > 0 {
			if overall, err = readWindow(tx, from); err != nil {
				return err
			}
		}
		if lim.PerPerson > 0 {
			for _, to := range order {
				if pairs[to], err = readWindow(tx, from.Collection(pairsCollection).Doc(to)); err != nil {
					return err
				}
			}
		}

		newOverall, newPairs, limitErr := plan(lim, now, actions, overall, pairs)
		if limitErr != nil {
			return limitErr
		}

		type counters struct{ propsies, dropsies int }
		received := map[string]counters{}
		var given counters
		for _, a := range actions {
			if err := tx.Create(f.user(a.To).Collection(receivedCollection).NewDoc(), a); err != nil {
				return err
			}
			c := received[a.To]
			if a.Delta > 0 {
				c.propsies++
				given.propsies++
			} else {
				c.dropsies++
				given.dropsies++
			}
			received[a.To] = c
		}
		for to, c := range received {
			err := tx.Set(f.user(to), map[string]any{
				"propsiesReceived": firestore.Increment(c.propsies),
				"dropsiesReceived": firestore.Increment(c.dropsies),
				"score":            firestore.Increment(c.propsies - c.dropsies),
			}, firestore.MergeAll)
			if err != nil {
				return err
			}
		}

		giver := map[string]any{
			"propsiesGiven": firestore.Increment(given.propsies),
			"dropsiesGiven": firestore.Increment(given.dropsies),
		}
		if lim.Overall > 0 {
			giver["windowStart"], giver["windowUsed"] = newOverall.Start, newOverall.Used
		}
		if err := tx.Set(from, giver, firestore.MergeAll); err != nil {
			return err
		}
		for to, w := range newPairs {
			err := tx.Set(from.Collection(pairsCollection).Doc(to),
				map[string]any{"windowStart": w.Start, "windowUsed": w.Used}, firestore.MergeAll)
			if err != nil {
				return err
			}
		}
		return nil
	})
	var limitErr *LimitError
	if errors.As(err, &limitErr) {
		return limitErr
	}
	if err != nil {
		return fmt.Errorf("add actions: %w", err)
	}
	return nil
}

// Allowance implements Repository.
func (f *Firestore) Allowance(ctx context.Context, userID string, period time.Duration, now time.Time) (Allowance, error) {
	snap, err := f.user(userID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Allowance{}, nil
	}
	if err != nil {
		return Allowance{}, fmt.Errorf("allowance: %w", err)
	}
	var d windowDoc
	if err := snap.DataTo(&d); err != nil {
		return Allowance{}, fmt.Errorf("decode allowance: %w", err)
	}
	w := d.window().live(now, period)
	return Allowance{Used: w.Used, ResetsAt: w.resetsAt(period)}, nil
}

// Stats implements Repository.
func (f *Firestore) Stats(ctx context.Context, userID string) (Stats, error) {
	snap, err := f.user(userID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Stats{}, nil
	}
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	var d struct {
		PropsiesReceived int `firestore:"propsiesReceived"`
		DropsiesReceived int `firestore:"dropsiesReceived"`
		PropsiesGiven    int `firestore:"propsiesGiven"`
		DropsiesGiven    int `firestore:"dropsiesGiven"`
	}
	if err := snap.DataTo(&d); err != nil {
		return Stats{}, fmt.Errorf("decode stats: %w", err)
	}
	return Stats(d), nil
}

// Leaderboard implements Repository.
func (f *Firestore) Leaderboard(ctx context.Context, limit int) ([]Entry, error) {
	it := f.client.Collection(usersCollection).
		OrderBy("score", firestore.Desc).Limit(limit).Documents(ctx)
	defer it.Stop()
	var out []Entry
	for {
		snap, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("leaderboard: %w", err)
		}
		var d struct {
			Score int `firestore:"score"`
		}
		if err := snap.DataTo(&d); err != nil {
			return nil, fmt.Errorf("decode leaderboard: %w", err)
		}
		out = append(out, Entry{UserID: snap.Ref.ID, Score: d.Score})
	}
}

// Recent implements Repository.
func (f *Firestore) Recent(ctx context.Context, userID string, limit int) ([]Action, error) {
	it := f.user(userID).Collection(receivedCollection).
		OrderBy("createdAt", firestore.Desc).Limit(limit).Documents(ctx)
	defer it.Stop()
	var out []Action
	for {
		snap, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("recent: %w", err)
		}
		var p Action
		if err := snap.DataTo(&p); err != nil {
			return nil, fmt.Errorf("decode action: %w", err)
		}
		out = append(out, p)
	}
}

// Close implements Repository.
func (f *Firestore) Close() error { return f.client.Close() }
