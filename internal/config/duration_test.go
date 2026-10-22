package config

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"30s":   30 * time.Second,
		"5m":    5 * time.Minute,
		"24h":   24 * time.Hour,
		"30d":   30 * 24 * time.Hour,
		"2w":    14 * 24 * time.Hour,
		"1d12h": 36 * time.Hour,
		" 7D ":  7 * 24 * time.Hour,
	}
	for in, want := range ok {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "30", "d", "0d", "-1d", "1.5h", "30days", "1x", "h5", "99999999999d"} {
		if got, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want error", in, got)
		}
	}
}
