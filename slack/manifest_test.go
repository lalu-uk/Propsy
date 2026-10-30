package slack_test

import (
	"os"
	"strings"
	"testing"
)

// normalize drops comments and the /dropsy command block so a Dropsy variant
// can be compared against its base manifest.
func normalize(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // fixed repo paths
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	inDropsy := false
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
		case strings.HasPrefix(line, "    - command: /dropsy"):
			inDropsy = true
		case inDropsy && strings.HasPrefix(line, "      "):
		default:
			inDropsy = false
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// The Dropsy variants must differ from the default manifests only by the
// /dropsy command, so the two copies can't silently drift apart.
func TestDropsyManifestsMatchBase(t *testing.T) {
	for _, mode := range []string{"socket", "http"} {
		ext := ".yaml"
		if mode == "http" {
			ext = ".yaml.tftpl" // template
		}
		base := "manifest." + mode + ext
		variant := "manifest." + mode + ".dropsy" + ext

		if b, _ := os.ReadFile(base); strings.Contains(string(b), "dropsy") { //nolint:gosec // fixed repo paths
			t.Errorf("%s must not mention dropsy: it's opt-in", base)
		}
		if v, _ := os.ReadFile(variant); !strings.Contains(string(v), "command: /dropsy") { //nolint:gosec // fixed repo paths
			t.Errorf("%s is missing the /dropsy command", variant)
		}
		if normalize(t, base) != normalize(t, variant) {
			t.Errorf("%s differs from %s by more than the /dropsy command", variant, base)
		}
	}
}
