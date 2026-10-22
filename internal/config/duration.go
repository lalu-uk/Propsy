package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var units = map[string]time.Duration{
	"s": time.Second,
	"m": time.Minute,
	"h": time.Hour,
	"d": 24 * time.Hour,
	"w": 7 * 24 * time.Hour,
}

// ParseDuration parses a human-friendly duration: one or more integer+unit
// pairs, where unit is s, m, h, d (24h) or w (7d). Examples: "30s", "24h",
// "30d", "2w", "1d12h". It extends time.ParseDuration, which has no days.
// The result must be positive.
func ParseDuration(s string) (time.Duration, error) {
	orig := s
	s = strings.TrimSpace(s)
	var total time.Duration
	for s != "" {
		i := 0
		for i < len(s) && unicode.IsDigit(rune(s[i])) {
			i++
		}
		j := i
		for j < len(s) && unicode.IsLetter(rune(s[j])) {
			j++
		}
		unit, ok := units[strings.ToLower(s[i:j])]
		if i == 0 || !ok {
			return 0, fmt.Errorf("invalid duration %q: want e.g. 30s, 24h, 30d, 2w", orig)
		}
		n, err := strconv.ParseInt(s[:i], 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", orig, err)
		}
		total += time.Duration(n) * unit
		s = s[j:]
	}
	if total <= 0 {
		return 0, fmt.Errorf("invalid duration %q: must be positive", orig)
	}
	return total, nil
}
