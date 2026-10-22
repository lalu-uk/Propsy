// Package config loads Propsy's runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Environment selects which deployment mode Propsy runs in (see PLAN.md §2).
const (
	EnvLocal = "local" // Socket Mode + SQLite (default)
	EnvGCP   = "gcp"   // HTTP webhooks + Firestore, for Cloud Run
)

// Config holds runtime settings. Fields unused by the active mode are zero.
type Config struct {
	Environment string // EnvLocal | EnvGCP

	SlackBotToken string // xoxb-..., both modes

	// Rate limits on propsies and dropsies a user can give. Each period is a
	// fixed window that opens on first use and resets the allowance in full
	// once it has elapsed (see store.Limits); a limit of 0 means unlimited.
	// Limit is the overall allowance, PerPersonLimit caps how many one person
	// can get from one giver.
	Limit                int
	LimitPeriod          time.Duration
	PerPersonLimit       int
	PerPersonLimitPeriod time.Duration

	// AnnounceChannel, if set, is where propsies/dropsies are announced
	// instead of the channel the command was run in: a channel ID (C0123…,
	// recommended) or #name (public channels only).
	AnnounceChannel string

	// EnvLocal.
	SlackAppToken string // xapp-... (Socket Mode)
	DBPath        string // SQLite file path

	// EnvGCP.
	Port                string // Cloud Run sets this
	SlackSigningSecret  string // verifies inbound Slack requests
	GCPProjectID        string // Firestore project
	FirestoreDatabaseID string // usually "(default)"
}

// Load reads configuration from the environment, falling back to a ".env"
// file in the working directory for anything unset (real environment
// variables always win). It returns an error naming every variable missing
// for the selected Environment at once.
func Load() (*Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return nil, err
	}
	c := &Config{
		Environment:   envOr("ENVIRONMENT", EnvLocal),
		SlackBotToken: os.Getenv("SLACK_BOT_TOKEN"),
	}

	var err error
	if c.Limit, c.LimitPeriod, err = limitFromEnv("PROPSY_LIMIT", 200, "30d"); err != nil {
		return nil, err
	}
	if c.PerPersonLimit, c.PerPersonLimitPeriod, err = limitFromEnv("PROPSY_PER_PERSON_LIMIT", 5, "1d"); err != nil {
		return nil, err
	}
	c.AnnounceChannel = os.Getenv("PROPSY_ANNOUNCE_CHANNEL")

	var missing []string
	if c.SlackBotToken == "" {
		missing = append(missing, "SLACK_BOT_TOKEN")
	}

	switch c.Environment {
	case EnvGCP:
		c.Port = envOr("PORT", "8080")
		c.SlackSigningSecret = os.Getenv("SLACK_SIGNING_SECRET")
		c.GCPProjectID = os.Getenv("GCP_PROJECT_ID")
		c.FirestoreDatabaseID = envOr("FIRESTORE_DATABASE_ID", "(default)")
		if c.SlackSigningSecret == "" {
			missing = append(missing, "SLACK_SIGNING_SECRET")
		}
		if c.GCPProjectID == "" {
			missing = append(missing, "GCP_PROJECT_ID")
		}
	case EnvLocal:
		c.SlackAppToken = os.Getenv("SLACK_APP_TOKEN")
		c.DBPath = envOr("DB_PATH", "./propsy.db")
		if c.SlackAppToken == "" {
			missing = append(missing, "SLACK_APP_TOKEN")
		}
	default:
		return nil, fmt.Errorf("invalid ENVIRONMENT %q: want %q or %q", c.Environment, EnvLocal, EnvGCP)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables for ENVIRONMENT=%s: %v", c.Environment, missing)
	}
	return c, nil
}

// limitFromEnv reads a rate limit from <prefix> (a count, 0 = unlimited) and
// <prefix>_PERIOD (a human-friendly duration, see ParseDuration).
func limitFromEnv(prefix string, defLimit int, defPeriod string) (int, time.Duration, error) {
	limit, err := envInt(prefix, defLimit)
	if err != nil {
		return 0, 0, err
	}
	if limit < 0 {
		return 0, 0, fmt.Errorf("%s must be >= 0 (0 = unlimited), got %d", prefix, limit)
	}
	period, err := ParseDuration(envOr(prefix+"_PERIOD", defPeriod))
	if err != nil {
		return 0, 0, fmt.Errorf("%s_PERIOD: %w", prefix, err)
	}
	return limit, period, nil
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", key, v)
	}
	return n, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
