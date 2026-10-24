package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testRepository is the contract every Repository implementation must meet.
// uniq prefixes user IDs so a shared (emulator) database can be reused
// across runs without cleanup.
//
//nolint:gocyclo // one long linear script of assertions; splitting it would only obscure the data flow
func testRepository(t *testing.T, r Repository, uniq string) {
	t.Helper()
	ctx := context.Background()
	a, b, c := uniq+"A", uniq+"B", uniq+"C"
	base := time.Now().UTC().Truncate(time.Millisecond)

	st, err := r.Stats(ctx, a)
	if err != nil || st != (Stats{}) {
		t.Fatalf("unknown user Stats = %+v, %v; want zero", st, err)
	}

	actions := []Action{
		{From: a, To: b, Reason: "first", ChannelID: "C1", CreatedAt: base, Delta: 1},
		{From: c, To: b, Reason: "second", ChannelID: "C1", CreatedAt: base.Add(time.Second), Delta: 1},
		{From: b, To: a, Reason: "", ChannelID: "C2", CreatedAt: base.Add(2 * time.Second), Delta: 1},
		{From: c, To: b, Reason: "third", ChannelID: "C1", CreatedAt: base.Add(3 * time.Second), Delta: 1},
		{From: a, To: b, Reason: "dropped", ChannelID: "C1", CreatedAt: base.Add(4 * time.Second), Delta: -1},
		{From: b, To: c, Reason: "nope", ChannelID: "C1", CreatedAt: base.Add(5 * time.Second), Delta: -1},
	}
	for _, p := range actions {
		if err := r.AddActions(ctx, []Action{p}, Limits{}, p.CreatedAt); err != nil {
			t.Fatalf("AddActions: %v", err)
		}
	}
	for _, d := range []int{0, 2} {
		if err := r.AddActions(ctx, []Action{{From: a, To: b, Delta: d, CreatedAt: base}}, Limits{}, base); !errors.Is(err, ErrBadDelta) {
			t.Errorf("AddActions(delta=%d) err = %v, want ErrBadDelta", d, err)
		}
	}

	for user, want := range map[string]Stats{
		a: {PropsiesReceived: 1, PropsiesGiven: 1, DropsiesGiven: 1},
		b: {PropsiesReceived: 3, DropsiesReceived: 1, PropsiesGiven: 1, DropsiesGiven: 1},
		c: {DropsiesReceived: 1, PropsiesGiven: 2},
	} {
		got, err := r.Stats(ctx, user)
		if err != nil || got != want {
			t.Errorf("Stats(%s) = %+v, %v; want %+v", user, got, err, want)
		}
	}

	recent, err := r.Recent(ctx, b, 2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 || recent[0].Reason != "dropped" || recent[1].Reason != "third" {
		t.Fatalf("Recent(b, 2) = %+v; want dropped, third", recent)
	}
	if got := recent[0]; got.From != a || got.To != b || got.ChannelID != "C1" || got.Delta != -1 || !got.CreatedAt.Equal(base.Add(4*time.Second)) {
		t.Errorf("Recent round-trip mismatch: %+v", got)
	}

	// Shared databases may hold other users; just check relative order of ours.
	board, err := r.Leaderboard(ctx, BoardReceived, 1000)
	if err != nil {
		t.Fatalf("Leaderboard: %v", err)
	}
	var ours []Entry
	for _, e := range board {
		if e.UserID == a || e.UserID == b || e.UserID == c {
			ours = append(ours, e)
		}
	}
	if got := (Stats{PropsiesReceived: 3, DropsiesReceived: 1}).Score(); got != 2 {
		t.Errorf("Score() = %d, want 2", got)
	}
	if len(ours) != 3 || ours[0] != (Entry{b, 2}) || ours[1] != (Entry{a, 1}) || ours[2] != (Entry{c, -1}) {
		t.Errorf("Leaderboard (ours) = %+v; want b:2, a:1, c:-1 (negative scores are listed)", ours)
	}

	if top, err := r.Leaderboard(ctx, BoardReceived, 1); err != nil || len(top) != 1 {
		t.Errorf("Leaderboard(received, 1) = %+v, %v; want 1 entry", top, err)
	}
}

func TestJSONFileRepository(t *testing.T) {
	for name, path := range map[string]string{
		"memory": "",
		"file":   filepath.Join(t.TempDir(), "propsy.json"),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := NewJSONFile(path)
			if err != nil {
				t.Fatalf("NewJSONFile: %v", err)
			}
			t.Cleanup(func() { _ = s.Close() })
			testRepository(t, s, "json")
			testLimits(t, s, "json")
			testBoards(t, s, "json")
		})
	}
}

// The Firestore test talks to a local emulator
// (`source ./firestore.sh start`) and skips when FIRESTORE_EMULATOR_HOST is
// unset. It never touches live GCP.
func TestFirestoreRepository(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping Firestore emulator test")
	}
	f, err := NewFirestore(context.Background(), "propsy-test", "(default)")
	if err != nil {
		t.Fatalf("NewFirestore: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	uniq := time.Now().Format("150405.000000") + "-"
	testRepository(t, f, uniq)
	testLimits(t, f, uniq)
	testBoards(t, f, uniq)
}

// testLimits is the rate-limit contract: fixed windows that open on first use,
// reset in full once the period has elapsed, and are enforced atomically.
//
//nolint:gocyclo // one long linear script of assertions; splitting it would only obscure the data flow
func testLimits(t *testing.T, r Repository, uniq string) {
	t.Helper()
	ctx := context.Background()
	g, x, y, z := uniq+"LG", uniq+"LX", uniq+"LY", uniq+"LZ"
	base := time.Now().UTC().Truncate(time.Millisecond)
	day := 24 * time.Hour
	lim := Limits{Overall: 3, OverallPeriod: day, PerPerson: 2, PerPersonPeriod: day}

	give := func(at time.Time, to ...string) error {
		actions := make([]Action, 0, len(to))
		for _, id := range to {
			actions = append(actions, Action{From: g, To: id, ChannelID: "C1", CreatedAt: at, Delta: 1})
		}
		return r.AddActions(ctx, actions, lim, at)
	}
	received := func(id string) int {
		st, err := r.Stats(ctx, id)
		if err != nil {
			t.Fatalf("Stats(%s): %v", id, err)
		}
		return st.PropsiesReceived
	}
	wantLimit := func(err error, kind LimitKind) *LimitError {
		t.Helper()
		var le *LimitError
		if !errors.As(err, &le) || le.Kind != kind {
			t.Fatalf("err = %v, want a LimitError of kind %d", err, kind)
		}
		return le
	}

	if al, err := r.Allowance(ctx, g, day, base); err != nil || al != (Allowance{}) {
		t.Fatalf("fresh Allowance = %+v, %v; want zero", al, err)
	}

	// Two to X: the per-person cap (2) is now reached.
	if err := give(base, x); err != nil {
		t.Fatal(err)
	}
	if err := give(base.Add(time.Hour), x); err != nil {
		t.Fatal(err)
	}
	le := wantLimit(give(base.Add(2*time.Hour), x), LimitPerPerson)
	if len(le.Capped) != 1 || le.Capped[0].UserID != x || !le.Capped[0].ResetsAt.Equal(base.Add(day)) || le.Limit != 2 {
		t.Errorf("per-person error = %+v; want X capped, resetting at the window's end (first use + 1d)", le)
	}

	// 2 more would make 4 > 3 overall: all-or-nothing, nothing recorded.
	le = wantLimit(give(base.Add(2*time.Hour), y, z), LimitOverall)
	if le.Left != 1 || le.Limit != 3 || !le.ResetsAt.Equal(base.Add(day)) {
		t.Errorf("overall error = %+v; want 1 left, resetting at base+1d", le)
	}
	if received(y) != 0 || received(z) != 0 {
		t.Errorf("a rejected command must record nothing")
	}

	if err := give(base.Add(3*time.Hour), y); err != nil {
		t.Fatal(err)
	}
	al, err := r.Allowance(ctx, g, day, base.Add(4*time.Hour))
	if err != nil || al.Used != 3 || !al.ResetsAt.Equal(base.Add(day)) {
		t.Errorf("Allowance = %+v, %v; want 3 used, resets base+1d", al, err)
	}
	_ = wantLimit(give(base.Add(4*time.Hour), z), LimitOverall)

	// Once the period has elapsed, everything resets in one go (the window is
	// fixed from first use, not rolling: the propsies given at +1h and +3h
	// don't keep counting).
	reset := base.Add(day)
	if err := give(reset, x, y); err != nil {
		t.Fatalf("after reset: %v", err)
	}
	if al, err = r.Allowance(ctx, g, day, reset.Add(time.Hour)); err != nil || al.Used != 2 || !al.ResetsAt.Equal(reset.Add(day)) {
		t.Errorf("Allowance after reset = %+v, %v; want 2 used, new window resetting at %v", al, err, reset.Add(day))
	}
	// A later action doesn't move the window's end.
	if err := give(reset.Add(5*time.Hour), z); err != nil {
		t.Fatal(err)
	}
	if al, _ = r.Allowance(ctx, g, day, reset.Add(6*time.Hour)); al.Used != 3 || !al.ResetsAt.Equal(reset.Add(day)) {
		t.Errorf("Allowance = %+v; want 3 used, window end unchanged", al)
	}
	// A window that has lapsed reads as unused.
	if al, _ = r.Allowance(ctx, g, day, reset.Add(2*day)); al != (Allowance{}) {
		t.Errorf("lapsed Allowance = %+v, want zero", al)
	}

	// Limits{} means unlimited and tracks no windows.
	free := uniq + "LF"
	for i := 0; i < 5; i++ {
		a := Action{From: free, To: x, CreatedAt: base, Delta: 1}
		if err := r.AddActions(ctx, []Action{a}, Limits{}, base); err != nil {
			t.Fatalf("unlimited AddActions: %v", err)
		}
	}
	if al, _ := r.Allowance(ctx, free, day, base); al.Used != 0 {
		t.Errorf("unlimited giver Allowance = %+v, want zero", al)
	}
}

// testBoards: the received board covers everyone who has received a propsy or
// dropsy (even at a negative score), the given board everyone who has given
// one, and neither is capped except by the limit asked for.
func testBoards(t *testing.T, r Repository, uniq string) {
	t.Helper()
	ctx := context.Background()
	p, q, rr, s := uniq+"BP", uniq+"BQ", uniq+"BR", uniq+"BS"
	base := time.Now().UTC().Truncate(time.Millisecond)

	for i, a := range []Action{
		{From: p, To: q, Delta: 1},
		{From: p, To: q, Delta: 1},
		{From: rr, To: q, Delta: -1}, // rr has only ever given a dropsy
		{From: q, To: s, Delta: -1},  // s is only ever on the receiving end of a dropsy
	} {
		a.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := r.AddActions(ctx, []Action{a}, Limits{}, a.CreatedAt); err != nil {
			t.Fatal(err)
		}
	}

	// ours returns the entries for our users, plus a map of their counts.
	ours := func(board Board) (map[string]int, []Entry) {
		t.Helper()
		all, err := r.Leaderboard(ctx, board, 1000)
		if err != nil {
			t.Fatalf("Leaderboard(%d): %v", board, err)
		}
		m := map[string]int{}
		var order []Entry
		for _, e := range all {
			if e.UserID == p || e.UserID == q || e.UserID == rr || e.UserID == s {
				m[e.UserID] = e.Count
				order = append(order, e)
			}
		}
		return m, order
	}

	got, order := ours(BoardReceived)
	if len(got) != 2 || got[q] != 1 || got[s] != -1 || order[0].UserID != q {
		t.Errorf("received board = %+v; want q:1 then s:-1, and not p or r (who've received nothing)", order)
	}

	got, order = ours(BoardGiven)
	if len(got) != 3 || got[p] != 2 || got[q] != 1 || got[rr] != 1 ||
		order[0].UserID != p || order[1].UserID != q || order[2].UserID != rr {
		t.Errorf("given board = %+v; want p:2, then q:1 and r:1 in user ID order (dropsies given count too), and not s", order)
	}

	if top, err := r.Leaderboard(ctx, BoardGiven, 1); err != nil || len(top) != 1 {
		t.Errorf("Leaderboard(given, 1) = %+v, %v; want 1 entry", top, err)
	}
	if _, err := r.Leaderboard(ctx, Board(99), 10); err == nil {
		t.Error("an unknown board should be an error")
	}
}
