package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// jsonFileVersion is bumped if the file layout ever changes incompatibly.
const jsonFileVersion = 1

// sqliteMagic is how every SQLite database file starts. Earlier versions kept
// local data in one, and pointing the JSON store at it deserves a clear error.
const sqliteMagic = "SQLite format 3\x00"

// JSONFile is a Repository for local (single-process) use. It keeps
// everything in memory and mirrors the Firestore layout: a record per user (counters, the
// giver's overall rate-limit window, per-person windows and the propsies and
// dropsies received) and one pair of leaderboards. Every AddActions saves the
// whole thing by writing a temp file, syncing it and renaming it over the old
// one, so a crash never leaves a partial file. That rewrite is fine at this
// app's scale (roughly 1.5 MB at 10,000 propsies).
//
// It is safe for concurrent use within one process, but two processes must not
// share a file: the last writer would win.
type JSONFile struct {
	mu   sync.Mutex
	path string // "" keeps the data in memory only (tests)
	data fileData

	// write persists the file's bytes; a field so tests can make it fail.
	write func(path string, b []byte) error
}

// fileData is the whole file.
type fileData struct {
	Version int              `json:"version"`
	Users   map[string]*user `json:"users"`
	Boards  boards           `json:"boards"`
}

// user mirrors Firestore's users/{id} document and its subcollections.
type user struct {
	Stats
	Window   Window            `json:"window"`             // overall rate-limit window (as giver)
	Pairs    map[string]Window `json:"pairs,omitempty"`    // per-person windows, by recipient
	Received []Action          `json:"received,omitempty"` // oldest first
}

// boards mirrors Firestore's boards/all document.
type boards struct {
	Received map[string]int `json:"received"` // user ID -> score
	Given    map[string]int `json:"given"`    // user ID -> propsies and dropsies given
}

// NewJSONFile opens the store at path, creating it on the first write. An empty
// path keeps everything in memory.
func NewJSONFile(path string) (*JSONFile, error) {
	s := &JSONFile{path: path, write: atomicWrite, data: newFileData()}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is the configured data file
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.HasPrefix(b, []byte(sqliteMagic)) {
		return nil, fmt.Errorf("%s is an old SQLite database: Propsy now keeps local data in a JSON file and doesn't migrate it; delete it or point DB_PATH at a new file", path)
	}
	var d fileData
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if d.Version != jsonFileVersion {
		return nil, fmt.Errorf("%s has version %d, this build understands %d", path, d.Version, jsonFileVersion)
	}
	if d.Users == nil {
		d.Users = map[string]*user{}
	}
	if d.Boards.Received == nil {
		d.Boards.Received = map[string]int{}
	}
	if d.Boards.Given == nil {
		d.Boards.Given = map[string]int{}
	}
	s.data = d
	return s, nil
}

func newFileData() fileData {
	return fileData{
		Version: jsonFileVersion,
		Users:   map[string]*user{},
		Boards:  boards{Received: map[string]int{}, Given: map[string]int{}},
	}
}

// AddActions implements Repository. It works on a copy of what it touches and
// only swaps it in once the file is saved, so a failed save changes nothing.
func (s *JSONFile) AddActions(_ context.Context, actions []Action, lim Limits, now time.Time) error {
	if len(actions) == 0 {
		return nil
	}
	for _, a := range actions {
		if a.Delta != 1 && a.Delta != -1 {
			return ErrBadDelta
		}
	}
	from := actions[0].From
	order, _ := recipients(actions)

	s.mu.Lock()
	defer s.mu.Unlock()

	var overall Window
	pairs := map[string]Window{}
	if giver := s.data.Users[from]; giver != nil {
		overall = giver.Window
		for _, to := range order {
			pairs[to] = giver.Pairs[to]
		}
	}
	newOverall, newPairs, limitErr := plan(lim, now, actions, overall, pairs)
	if limitErr != nil {
		return limitErr
	}

	next := s.data.cloneShallow()
	touched := map[string]*user{}
	touch := func(id string) *user {
		if u, ok := touched[id]; ok {
			return u
		}
		u := &user{}
		if old := next.Users[id]; old != nil {
			c := *old
			c.Pairs = maps0(old.Pairs)
			u = &c
		}
		touched[id], next.Users[id] = u, u
		return u
	}

	for _, a := range actions {
		to := touch(a.To)
		to.Received = append(to.Received, a)
		giver := touch(from)
		if a.Delta > 0 {
			to.PropsiesReceived++
			giver.PropsiesGiven++
		} else {
			to.DropsiesReceived++
			giver.DropsiesGiven++
		}
		next.Boards.Received[a.To] += a.Delta
		next.Boards.Given[from]++
	}
	giver := touch(from)
	if lim.Overall > 0 {
		giver.Window = newOverall
	}
	if len(newPairs) > 0 && giver.Pairs == nil {
		giver.Pairs = map[string]Window{}
	}
	for to, w := range newPairs {
		giver.Pairs[to] = w
	}

	if err := s.save(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// cloneShallow copies the maps but shares the user records; AddActions copies
// the ones it changes.
func (d fileData) cloneShallow() fileData {
	return fileData{
		Version: d.Version,
		Users:   maps0(d.Users),
		Boards:  boards{Received: maps0(d.Boards.Received), Given: maps0(d.Boards.Given)},
	}
}

func maps0[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *JSONFile) save(d fileData) error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("encode data: %w", err)
	}
	if err := s.write(s.path, b); err != nil {
		return fmt.Errorf("save %s: %w", s.path, err)
	}
	return nil
}

// atomicWrite replaces path with b so that a crash leaves either the old file
// or the new one, never a partial one.
func atomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Persist the rename itself. Best effort: not every platform lets you open a directory.
	if d, derr := os.Open(dir); derr == nil { //nolint:gosec // dir of the configured data file
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Allowance implements Repository.
func (s *JSONFile) Allowance(_ context.Context, userID string, period time.Duration, now time.Time) (Allowance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.data.Users[userID]
	if u == nil {
		return Allowance{}, nil
	}
	w := u.Window.live(now, period)
	return Allowance{Used: w.Used, ResetsAt: w.resetsAt(period)}, nil
}

// PairAllowance implements Repository.
func (s *JSONFile) PairAllowance(_ context.Context, from, to string, period time.Duration, now time.Time) (Allowance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.data.Users[from]
	if u == nil {
		return Allowance{}, nil
	}
	w := u.Pairs[to].live(now, period)
	return Allowance{Used: w.Used, ResetsAt: w.resetsAt(period)}, nil
}

// Stats implements Repository.
func (s *JSONFile) Stats(_ context.Context, userID string) (Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.data.Users[userID]; u != nil {
		return u.Stats, nil
	}
	return Stats{}, nil
}

// Leaderboard implements Repository.
func (s *JSONFile) Leaderboard(_ context.Context, board Board, limit int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch board {
	case BoardReceived:
		return topEntries(s.data.Boards.Received, limit), nil
	case BoardGiven:
		return topEntries(s.data.Boards.Given, limit), nil
	}
	return nil, fmt.Errorf("leaderboard: unknown board %d", board)
}

// Recent implements Repository.
func (s *JSONFile) Recent(_ context.Context, userID string, limit int) ([]Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.data.Users[userID]
	if u == nil {
		return nil, nil
	}
	out := slices.Clone(u.Received)
	slices.Reverse(out) // newest-inserted first, so equal timestamps keep that order
	slices.SortStableFunc(out, func(a, b Action) int { return b.CreatedAt.Compare(a.CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Close implements Repository. Every write is already on disk.
func (s *JSONFile) Close() error { return nil }

// Ping implements Repository. The file is in memory, so there's nothing to reach.
func (s *JSONFile) Ping(context.Context) error { return nil }
