// Package config loads Propsy's runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"
)

// Environment selects which deployment mode Propsy runs in (see PLAN.md §2).
const (
	EnvLocal = "local" // Socket Mode + a JSON data file (default)
	EnvGCP   = "gcp"   // HTTP webhooks + Firestore, for Cloud Run
)

// MaxLeaderboardUsers is the most people a leaderboard ever lists, whatever
// PROPSY_LEADERBOARD_MAX_USERS says: 1000 rows is ~13 blocks, and Slack rejects
// a Home tab over 100 blocks.
const MaxLeaderboardUsers = 1000

// Config holds runtime settings. Fields unused by the active mode are zero.
type Config struct {
	Environment string // EnvLocal | EnvGCP

	SlackBotToken string // xoxb-..., both modes

	// Rate limits on Propsies and Dropsies a user can give. Each period is a
	// fixed window that opens on first use and resets the allowance in full
	// once it has elapsed (see store.Limits); a limit of 0 means unlimited.
	// Limit is the overall allowance, PerPersonLimit caps how many one person
	// can get from one giver.
	Limit                int
	LimitPeriod          time.Duration
	PerPersonLimit       int
	PerPersonLimitPeriod time.Duration

	// LeaderboardMaxUsers caps how many people each leaderboard lists; 0 lists
	// everyone, up to MaxLeaderboardUsers, which also bounds any larger value.
	LeaderboardMaxUsers int

	// Warnings are non-fatal notes about the configuration (a value that was
	// adjusted, say) for the caller to log.
	Warnings []string

	// AnnounceChannel, if set, is where Propsies/Dropsies are announced
	// instead of the channel the command was run in: a channel ID (C0123…,
	// recommended) or #name (public channels only).
	AnnounceChannel string

	// EnvLocal.
	SlackAppToken string // xapp-... (Socket Mode)
	DBPath        string // JSON data file path

	// EnvGCP.
	Port                string // Cloud Run sets this
	SlackSigningSecret  string // verifies inbound Slack requests
	GCPProjectID        string // Firestore project
	FirestoreDatabaseID string // usually "(default)"
	// DBPrefix namespaces the Firestore collections (propsy_users, ...), so
	// Propsy can share a database with other apps. Lowercase letters, digits
	// and underscores, starting with a letter.
	DBPrefix string
}

var dbPrefixRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

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
	if c.LeaderboardMaxUsers, err = envInt("PROPSY_LEADERBOARD_MAX_USERS", 0); err != nil {
		return nil, err
	}
	if c.LeaderboardMaxUsers < 0 {
		return nil, fmt.Errorf("PROPSY_LEADERBOARD_MAX_USERS must be >= 0 (0 = everyone), got %d", c.LeaderboardMaxUsers)
	}
	if c.LeaderboardMaxUsers > MaxLeaderboardUsers {
		c.Warnings = append(c.Warnings, fmt.Sprintf("PROPSY_LEADERBOARD_MAX_USERS=%d is above the maximum of %d; using %d",
			c.LeaderboardMaxUsers, MaxLeaderboardUsers, MaxLeaderboardUsers))
		c.LeaderboardMaxUsers = MaxLeaderboardUsers
	}

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
		if c.FirestoreDatabaseID != "(default)" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("FIRESTORE_DATABASE_ID=%q is a named database: Firestore's free quota only covers (default), so every read and write is billed",
				c.FirestoreDatabaseID))
		}
		c.DBPrefix = envOr("PROPSY_DB_PREFIX", "propsy")
		if !dbPrefixRE.MatchString(c.DBPrefix) {
			return nil, fmt.Errorf("PROPSY_DB_PREFIX=%q must be lowercase letters, digits and underscores, starting with a letter", c.DBPrefix)
		}
		if c.SlackSigningSecret == "" {
			missing = append(missing, "SLACK_SIGNING_SECRET")
		}
		if c.GCPProjectID == "" {
			missing = append(missing, "GCP_PROJECT_ID")
		}
	case EnvLocal:
		c.SlackAppToken = os.Getenv("SLACK_APP_TOKEN")
		c.DBPath = envOr("DB_PATH", "./propsy.json")
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
