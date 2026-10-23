package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadReportsAllMissing(t *testing.T) {
	for _, env := range []string{"", "gcp"} {
		t.Setenv("ENVIRONMENT", env)
		t.Setenv("SLACK_BOT_TOKEN", "")
		t.Setenv("SLACK_APP_TOKEN", "")
		t.Setenv("SLACK_SIGNING_SECRET", "")
		t.Setenv("GCP_PROJECT_ID", "")
		if _, err := Load(); err == nil {
			t.Errorf("ENVIRONMENT=%q: expected error for missing vars", env)
		}
	}
}

func TestLoadLocalDefaults(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-1")
	t.Setenv("SLACK_APP_TOKEN", "xapp-1")
	t.Setenv("DB_PATH", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Environment != EnvLocal || c.DBPath != "./propsy.db" {
		t.Errorf("got %+v", c)
	}
}

func TestLoadGCP(t *testing.T) {
	t.Setenv("ENVIRONMENT", "gcp")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-1")
	t.Setenv("SLACK_SIGNING_SECRET", "s")
	t.Setenv("GCP_PROJECT_ID", "proj")
	t.Setenv("PORT", "")
	t.Setenv("FIRESTORE_DATABASE_ID", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Port != "8080" || c.FirestoreDatabaseID != "(default)" || c.GCPProjectID != "proj" || len(c.Warnings) != 0 {
		t.Errorf("got %+v", c)
	}

	t.Setenv("FIRESTORE_DATABASE_ID", "propsy")
	if c, err = Load(); err != nil || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "free quota") {
		t.Errorf("named database: got warnings=%v, err=%v; want one free-quota warning", c.Warnings, err)
	}
}

func TestLoadInvalidEnvironment(t *testing.T) {
	t.Setenv("ENVIRONMENT", "mars")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-1")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

const (
	envLimit     = "PROPSY_LIMIT"
	envPerPerson = "PROPSY_PER_PERSON_LIMIT"
)

func TestLoadLimits(t *testing.T) {
	setup := func(env map[string]string) {
		t.Setenv("ENVIRONMENT", "")
		t.Setenv("SLACK_BOT_TOKEN", "xoxb-1")
		t.Setenv("SLACK_APP_TOKEN", "xapp-1")
		for _, k := range []string{
			envLimit, envLimit + "_PERIOD",
			envPerPerson, envPerPerson + "_PERIOD", "PROPSY_ANNOUNCE_CHANNEL",
		} {
			t.Setenv(k, env[k])
		}
	}
	day := 24 * time.Hour

	setup(nil)
	c, err := Load()
	if err != nil || c.Limit != 200 || c.LimitPeriod != 30*day || c.PerPersonLimit != 5 || c.PerPersonLimitPeriod != day || c.AnnounceChannel != "" {
		t.Errorf("defaults: %+v, %v; want 200/30d, 5/1d, no channel", c, err)
	}

	setup(map[string]string{
		envLimit: "7", envLimit + "_PERIOD": "12h",
		envPerPerson: "2", envPerPerson + "_PERIOD": "2w",
		"PROPSY_ANNOUNCE_CHANNEL": "C0123ABC",
	})
	c, err = Load()
	if err != nil || c.Limit != 7 || c.LimitPeriod != 12*time.Hour || c.PerPersonLimit != 2 || c.PerPersonLimitPeriod != 14*day || c.AnnounceChannel != "C0123ABC" {
		t.Errorf("custom: %+v, %v", c, err)
	}

	setup(map[string]string{envLimit: "0", envPerPerson: "0"})
	if c, err = Load(); err != nil || c.Limit != 0 || c.PerPersonLimit != 0 {
		t.Errorf("0 should mean unlimited: %+v, %v", c, err)
	}

	for _, bad := range []map[string]string{
		{envLimit: "-1"},
		{envLimit: "lots"},
		{envLimit + "_PERIOD": "soon"},
		{envLimit + "_PERIOD": "0d"},
		{envPerPerson: "-1"},
		{envPerPerson + "_PERIOD": "never"},
	} {
		setup(bad)
		if _, err := Load(); err == nil {
			t.Errorf("%v: expected error", bad)
		}
	}
}

func TestLoadLeaderboardMaxUsers(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-1")
	t.Setenv("SLACK_APP_TOKEN", "xapp-1")

	for in, want := range map[string]int{"": 0, "0": 0, "10": 10} {
		t.Setenv("PROPSY_LEADERBOARD_MAX_USERS", in)
		if c, err := Load(); err != nil || c.LeaderboardMaxUsers != want {
			t.Errorf("%q: got %+v, %v; want %d", in, c, err, want)
		}
	}
	// Above the ceiling it's lowered, with a warning rather than a failure.
	t.Setenv("PROPSY_LEADERBOARD_MAX_USERS", "5000")
	c, err := Load()
	if err != nil || c.LeaderboardMaxUsers != MaxLeaderboardUsers || len(c.Warnings) != 1 ||
		!strings.Contains(c.Warnings[0], "5000") || !strings.Contains(c.Warnings[0], "1000") {
		t.Errorf("5000: got max=%d warnings=%v, err=%v; want clamped to %d with one warning", c.LeaderboardMaxUsers, c.Warnings, err, MaxLeaderboardUsers)
	}
	// Exactly the ceiling is fine, and silent.
	t.Setenv("PROPSY_LEADERBOARD_MAX_USERS", "1000")
	if c, err = Load(); err != nil || c.LeaderboardMaxUsers != 1000 || len(c.Warnings) != 0 {
		t.Errorf("1000: got max=%d warnings=%v, err=%v", c.LeaderboardMaxUsers, c.Warnings, err)
	}

	for _, bad := range []string{"-1", "ten"} {
		t.Setenv("PROPSY_LEADERBOARD_MAX_USERS", bad)
		if _, err := Load(); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}
