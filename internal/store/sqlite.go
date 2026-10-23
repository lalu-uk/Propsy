package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

// SQLite is a Repository backed by a local SQLite database. (The table keeps
// its original name, "props", to avoid a rename migration.)
type SQLite struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS props (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	from_user  TEXT NOT NULL,
	to_user    TEXT NOT NULL,
	reason     TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	delta      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS props_to ON props (to_user, created_at);

-- Open rate-limit windows. other = '' is the giver's overall window, otherwise
-- it's the recipient of a per-person window.
CREATE TABLE IF NOT EXISTS windows (
	user_id TEXT NOT NULL,
	other   TEXT NOT NULL,
	start   INTEGER NOT NULL,
	used    INTEGER NOT NULL,
	PRIMARY KEY (user_id, other)
);`

// NewSQLite opens (creating if needed) a database at path. Use ":memory:" for
// an ephemeral store.
func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}
	// One connection keeps writes ordered (and makes ":memory:" a single DB).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

// migrate adds columns introduced after a database's initial creation
// (CREATE TABLE IF NOT EXISTS is a no-op against an existing table).
func migrate(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(props)`)
	if err != nil {
		return err
	}
	hasDelta := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			_ = rows.Close()
			return err
		}
		hasDelta = hasDelta || name == "delta"
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasDelta {
		_, err = db.Exec(`ALTER TABLE props ADD COLUMN delta INTEGER NOT NULL DEFAULT 1`)
	}
	return err
}

// AddActions implements Repository.
func (s *SQLite) AddActions(ctx context.Context, actions []Action, lim Limits, now time.Time) (err error) {
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("add actions: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var overall Window
	pairs := map[string]Window{}
	if lim.Overall > 0 {
		if overall, err = loadWindow(ctx, tx, from, ""); err != nil {
			return err
		}
	}
	if lim.PerPerson > 0 {
		for _, to := range order {
			if pairs[to], err = loadWindow(ctx, tx, from, to); err != nil {
				return err
			}
		}
	}

	newOverall, newPairs, limitErr := plan(lim, now, actions, overall, pairs)
	if limitErr != nil {
		return limitErr
	}

	for _, a := range actions {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO props (from_user, to_user, reason, channel_id, created_at, delta) VALUES (?, ?, ?, ?, ?, ?)`,
			a.From, a.To, a.Reason, a.ChannelID, a.CreatedAt.UnixMicro(), a.Delta)
		if err != nil {
			return fmt.Errorf("add actions: %w", err)
		}
	}
	if lim.Overall > 0 {
		if err = saveWindow(ctx, tx, from, "", newOverall); err != nil {
			return err
		}
	}
	for to, w := range newPairs {
		if err = saveWindow(ctx, tx, from, to, w); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("add actions: %w", err)
	}
	return nil
}

func loadWindow(ctx context.Context, tx *sql.Tx, user, other string) (Window, error) {
	var start int64
	var w Window
	err := tx.QueryRowContext(ctx, `SELECT start, used FROM windows WHERE user_id = ? AND other = ?`, user, other).Scan(&start, &w.Used)
	if errors.Is(err, sql.ErrNoRows) {
		return Window{}, nil
	}
	if err != nil {
		return Window{}, fmt.Errorf("load window: %w", err)
	}
	w.Start = time.UnixMicro(start)
	return w, nil
}

func saveWindow(ctx context.Context, tx *sql.Tx, user, other string, w Window) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO windows (user_id, other, start, used) VALUES (?, ?, ?, ?)
		 ON CONFLICT (user_id, other) DO UPDATE SET start = excluded.start, used = excluded.used`,
		user, other, w.Start.UnixMicro(), w.Used)
	if err != nil {
		return fmt.Errorf("save window: %w", err)
	}
	return nil
}

// Allowance implements Repository.
func (s *SQLite) Allowance(ctx context.Context, userID string, period time.Duration, now time.Time) (Allowance, error) {
	var start int64
	var w Window
	err := s.db.QueryRowContext(ctx, `SELECT start, used FROM windows WHERE user_id = ? AND other = ''`, userID).Scan(&start, &w.Used)
	if errors.Is(err, sql.ErrNoRows) {
		return Allowance{}, nil
	}
	if err != nil {
		return Allowance{}, fmt.Errorf("allowance: %w", err)
	}
	w.Start = time.UnixMicro(start)
	w = w.live(now, period)
	return Allowance{Used: w.Used, ResetsAt: w.resetsAt(period)}, nil
}

// Stats implements Repository.
func (s *SQLite) Stats(ctx context.Context, userID string) (Stats, error) {
	var st Stats
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(to_user = ? AND delta > 0), 0),
		        COALESCE(SUM(to_user = ? AND delta < 0), 0),
		        COALESCE(SUM(from_user = ? AND delta > 0), 0),
		        COALESCE(SUM(from_user = ? AND delta < 0), 0)
		 FROM props WHERE to_user = ? OR from_user = ?`,
		userID, userID, userID, userID, userID, userID).
		Scan(&st.PropsiesReceived, &st.DropsiesReceived, &st.PropsiesGiven, &st.DropsiesGiven)
	if err != nil {
		return Stats{}, fmt.Errorf("stats: %w", err)
	}
	return st, nil
}

// Leaderboard implements Repository.
func (s *SQLite) Leaderboard(ctx context.Context, board Board, limit int) ([]Entry, error) {
	var query string
	switch board {
	case BoardReceived:
		query = `SELECT to_user, SUM(delta) AS n FROM props GROUP BY to_user ORDER BY n DESC, to_user LIMIT ?`
	case BoardGiven:
		query = `SELECT from_user, COUNT(*) AS n FROM props GROUP BY from_user ORDER BY n DESC, from_user LIMIT ?`
	default:
		return nil, fmt.Errorf("leaderboard: unknown board %d", board)
	}
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("leaderboard: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.UserID, &e.Count); err != nil {
			return nil, fmt.Errorf("scan leaderboard: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Recent implements Repository.
func (s *SQLite) Recent(ctx context.Context, userID string, limit int) ([]Action, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_user, to_user, reason, channel_id, created_at, delta FROM props WHERE to_user = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Action
	for rows.Next() {
		var p Action
		var micro int64
		if err := rows.Scan(&p.From, &p.To, &p.Reason, &p.ChannelID, &micro, &p.Delta); err != nil {
			return nil, fmt.Errorf("scan recent: %w", err)
		}
		p.CreatedAt = time.UnixMicro(micro).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// Close implements Repository.
func (s *SQLite) Close() error { return s.db.Close() }
