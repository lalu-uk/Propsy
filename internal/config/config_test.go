package config

import (
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
	if c.Port != "8080" || c.FirestoreDatabaseID != "(default)" || c.GCPProjectID != "proj" {
		t.Errorf("got %+v", c)
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
