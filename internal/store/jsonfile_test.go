package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func jsonStore(t *testing.T, path string) *JSONFile {
	t.Helper()
	s, err := NewJSONFile(path)
	if err != nil {
		t.Fatalf("NewJSONFile(%q): %v", path, err)
	}
	return s
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a file in the test's temp dir
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func give(t *testing.T, s Repository, from, to string, delta int, at time.Time, lim Limits) error {
	t.Helper()
	return s.AddActions(context.Background(), []Action{{From: from, To: to, Reason: "r", ChannelID: "C1", Delta: delta, CreatedAt: at}}, lim, at)
}

func TestJSONFilePersistsAndReloads(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "dir", "propsy.json") // the directories don't exist yet
	now := time.Now().UTC().Truncate(time.Millisecond)
	lim := Limits{Overall: 5, OverallPeriod: time.Hour, PerPerson: 2, PerPersonPeriod: time.Hour}

	s := jsonStore(t, path)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a new store shouldn't write anything until the first give: %v", err)
	}
	for i, d := range []int{1, 1, -1} {
		if err := give(t, s, "UA", "UB", d, now.Add(time.Duration(i)*time.Second), lim); err != nil && i < 2 {
			t.Fatal(err)
		}
	}
	// the third give to UB is over the per-person limit of 2
	if err := give(t, s, "UA", "UC", 1, now.Add(3*time.Second), lim); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	b := readFile(t, path)
	var raw struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.Version != jsonFileVersion {
		t.Fatalf("file isn't valid JSON with a version: %v %s", err, b)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("expected only the data file, no leftover temp files: %v", entries)
	}

	r := jsonStore(t, path) // reopen from disk
	defer func() { _ = r.Close() }()
	if st, _ := r.Stats(ctx, "UB"); st.PropsiesReceived != 2 || st.DropsiesReceived != 0 {
		t.Errorf("UB stats after reload = %+v", st)
	}
	if st, _ := r.Stats(ctx, "UA"); st.PropsiesGiven != 3 {
		t.Errorf("UA stats after reload = %+v", st)
	}
	if al, _ := r.Allowance(ctx, "UA", time.Hour, now.Add(time.Minute)); al.Used != 3 || !al.ResetsAt.Equal(now.Add(time.Hour)) {
		t.Errorf("allowance after reload = %+v; want 3 used, resetting an hour after the first give", al)
	}
	board, _ := r.Leaderboard(ctx, BoardReceived, 10)
	if len(board) != 2 || board[0] != (Entry{"UB", 2}) || board[1] != (Entry{"UC", 1}) {
		t.Errorf("board after reload = %+v", board)
	}
	if recent, _ := r.Recent(ctx, "UB", 10); len(recent) != 2 || !recent[0].CreatedAt.Equal(now.Add(time.Second)) {
		t.Errorf("recent after reload = %+v", recent)
	}
	// the per-person window survived too: UB is capped for UA
	var le *LimitError
	if err := give(t, r, "UA", "UB", 1, now.Add(4*time.Second), lim); !errors.As(err, &le) || le.Kind != LimitPerPerson {
		t.Errorf("per-person window lost on reload: %v", err)
	}
}

func TestJSONFileRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, tc := range map[string]struct{ content, want string }{
		"corrupt":     {`{"version": 1, "users": {`, "parse"},
		"version":     {`{"version": 99}`, "version 99"},
		"sqlite":      {"SQLite format 3\x00\x10\x00\x01\x01", "old SQLite database"},
		"unversioned": {`{"users": {}}`, "version 0"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewJSONFile(write(name+".json", tc.content))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestJSONFileFailedSaveChangesNothing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "propsy.json")
	s := jsonStore(t, path)
	now := time.Now().UTC()
	if err := give(t, s, "UA", "UB", 1, now, Limits{}); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, path)

	s.write = func(string, []byte) error { return errors.New("disk full") }
	if err := give(t, s, "UA", "UB", 1, now.Add(time.Second), Limits{}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v, want the write failure", err)
	}
	if st, _ := s.Stats(ctx, "UB"); st.PropsiesReceived != 1 {
		t.Errorf("a failed save must leave memory unchanged, UB = %+v", st)
	}
	if board, _ := s.Leaderboard(ctx, BoardReceived, 10); len(board) != 1 || board[0].Count != 1 {
		t.Errorf("board changed by a failed save: %+v", board)
	}
	if string(readFile(t, path)) != string(before) {
		t.Error("the file changed despite the failed save")
	}

	s.write = atomicWrite // and it recovers
	if err := give(t, s, "UA", "UB", 1, now.Add(2*time.Second), Limits{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Stats(ctx, "UB"); st.PropsiesReceived != 2 {
		t.Errorf("UB = %+v, want 2 after recovering", st)
	}
}

func TestJSONFileLimitErrorWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "propsy.json")
	s := jsonStore(t, path)
	writes := 0
	s.write = func(p string, b []byte) error { writes++; return atomicWrite(p, b) }
	lim := Limits{Overall: 1, OverallPeriod: time.Hour}
	now := time.Now()
	if err := give(t, s, "UA", "UB", 1, now, lim); err != nil {
		t.Fatal(err)
	}
	var le *LimitError
	if err := give(t, s, "UA", "UC", 1, now, lim); !errors.As(err, &le) {
		t.Fatalf("err = %v, want a LimitError", err)
	}
	if writes != 1 {
		t.Errorf("writes = %d; a rejected give shouldn't touch the file", writes)
	}
}

func TestJSONFileConcurrentGives(t *testing.T) {
	s := jsonStore(t, filepath.Join(t.TempDir(), "propsy.json"))
	const n = 25
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := give(t, s, fmt.Sprintf("G%02d", i), "UTARGET", 1, time.Now(), Limits{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if st, _ := s.Stats(context.Background(), "UTARGET"); st.PropsiesReceived != n {
		t.Errorf("received = %d, want %d: concurrent gives were lost", st.PropsiesReceived, n)
	}
}

func TestJSONFileRecentOrdersByTimeThenLatestFirst(t *testing.T) {
	s := jsonStore(t, "")
	at := time.Now().UTC()
	for _, from := range []string{"U1", "U2", "U3"} { // same timestamp: the last one given comes first
		if err := give(t, s, from, "UB", 1, at, Limits{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := give(t, s, "U4", "UB", 1, at.Add(-time.Hour), Limits{}); err != nil { // older, but inserted last
		t.Fatal(err)
	}
	recent, _ := s.Recent(context.Background(), "UB", 3)
	got := make([]string, 0, len(recent))
	for _, a := range recent {
		got = append(got, a.From)
	}
	if strings.Join(got, ",") != "U3,U2,U1" {
		t.Errorf("recent = %v, want U3,U2,U1 (newest timestamp first, then latest inserted)", got)
	}
}
