package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// A database created before dropsies existed has no delta column; opening it
// must add one, with existing rows counting as +1.
func TestSQLiteMigratesPreDeltaDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE props (
		id INTEGER PRIMARY KEY AUTOINCREMENT, from_user TEXT NOT NULL, to_user TEXT NOT NULL,
		reason TEXT NOT NULL, channel_id TEXT NOT NULL, created_at INTEGER NOT NULL);
		INSERT INTO props (from_user, to_user, reason, channel_id, created_at) VALUES ('A', 'B', 'old', 'C', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.Close()

	s, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite on old schema: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	if st, err := s.Stats(ctx, "B"); err != nil || st.PropsiesReceived != 1 {
		t.Errorf("Stats(B) = %+v, %v; want 1 propsy received", st, err)
	}
	if err := s.AddActions(ctx, []Action{{From: "A", To: "B", Delta: -1, CreatedAt: time.Now()}}, Limits{}, time.Now()); err != nil {
		t.Fatalf("AddActions after migrate: %v", err)
	}
	// Reopening an already-migrated database is a no-op.
	_ = s.Close()
	s2, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	_ = s2.Close()
}
