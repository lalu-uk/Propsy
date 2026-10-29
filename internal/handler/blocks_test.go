package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/laluowen/Propsy/internal/store"
	"github.com/slack-go/slack"
)

func render(t *testing.T, blocks []slack.Block) string {
	t.Helper()
	b, err := json.Marshal(slack.Blocks{BlockSet: blocks})
	if err != nil {
		t.Fatal(err)
	}
	// encoding/json escapes < and > as \u003c / \u003e; undo for readable asserts.
	return strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(string(b))
}

func assertContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
}

func TestPluralNouns(t *testing.T) {
	for n, want := range map[int]string{0: "0 propsies", 1: "1 propsy", 2: "2 propsies", 10: "10 propsies"} {
		if got := propsies(n); got != want {
			t.Errorf("propsies(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int]string{0: "0 dropsies", 1: "1 dropsy", 5: "5 dropsies"} {
		if got := dropsies(n); got != want {
			t.Errorf("dropsies(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestHomeBlocksEmpty(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{}, boards{}, nil, nil))
	assertContains(t, out, "*Received:* 0 propsies", "*Given:* 0 propsies", "No propsies yet")
	for _, unwanted := range []string{"dropsies", "Recently received", "available"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%q should be hidden when empty/unlimited: %s", unwanted, out)
		}
	}
}

func TestHomeBlocksPopulated(t *testing.T) {
	board := []store.Entry{{UserID: "UA", Count: 5}, {UserID: "UB", Count: 4}, {UserID: "UC", Count: 3}, {UserID: "UD", Count: 1}}
	recent := []store.Action{
		{From: "UB", Reason: "thanks", CreatedAt: time.Unix(1700000000, 0).UTC(), Delta: 1},
		{From: "UC", Reason: "lol", CreatedAt: time.Unix(1700000000, 0).UTC(), Delta: -1},
	}
	stats := store.Stats{PropsiesReceived: 5, DropsiesReceived: 10, PropsiesGiven: 3, DropsiesGiven: 1}
	resets := time.Unix(1700000000, 0)
	q := &quota{used: 190, limit: 200, period: 30 * 24 * time.Hour, resetsAt: resets, perPerson: 5, perPersonPeriod: 24 * time.Hour}
	out := render(t, homeBlocks(stats, boards{received: board, given: []store.Entry{{UserID: "UB", Count: 7}}}, recent, q))
	assertContains(t, out,
		"*Received:* 5 propsies, 10 dropsies", "*Given:* 3 propsies, 1 dropsy",
		"Leaderboard: received", ":first_place_medal: <@UA> — 5", ":third_place_medal: <@UC> — 3", "4. <@UD> — 1",
		"Leaderboard: given", ":first_place_medal: <@UB> — 7",
		"Recently received", "<@UB> gave you a propsy: thanks", ":droplet: <@UC> gave you a dropsy: lol",
		"You have *10* propsies/dropsies available, resetting to 200 <!date^1700000000^{date_short_pretty} at {time}|2023-11-14 22:13 UTC>.",
		"You can give any one person up to 5 per 1d",
	)
}

func TestSingularNounsOnHome(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{PropsiesReceived: 1, DropsiesReceived: 1, PropsiesGiven: 1}, boards{}, nil, nil))
	assertContains(t, out, "*Received:* 1 propsy, 1 dropsy", "*Given:* 1 propsy")
}

func TestQuotaNeverShowsNegative(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{}, boards{}, nil, &quota{used: 205, limit: 200, period: 24 * time.Hour, resetsAt: time.Unix(1700000000, 0)}))
	assertContains(t, out, "*0* propsies/dropsies available, resetting to 200")
}

func TestQuotaWithOnlyPerPersonLimit(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{}, boards{}, nil, &quota{perPerson: 3, perPersonPeriod: 12 * time.Hour}))
	assertContains(t, out, "up to 3 per 12h0m0s")
	if strings.Contains(out, "available") {
		t.Errorf("no overall limit configured, so no 'available' line: %s", out)
	}
}

func TestAnnouncementBlocks(t *testing.T) {
	out := render(t, announcementBlocks("UA", []string{"UB", "UC"}, "line1\nline2", 1, 1))
	assertContains(t, out, ":raised_hands: <@UA> gave a propsy to <@UB>, <@UC>", ">line1\\n>line2")
	if strings.Contains(render(t, announcementBlocks("UA", []string{"UB"}, "", 1, 1)), `\n>`) {
		t.Error("no quote expected without a reason")
	}
	assertContains(t, render(t, announcementBlocks("UA", []string{"UB"}, "menace", -1, 1)),
		":droplet: <@UA> gave a dropsy to <@UB>", ">menace")
}

const hashKudos = "#kudos"

func TestChannelRef(t *testing.T) {
	for in, want := range map[string]string{
		"C0123ABC": "<#C0123ABC>", "G0123ABC": "<#G0123ABC>", hashKudos: hashKudos, "kudos": hashKudos,
	} {
		if got := channelRef(in); got != want {
			t.Errorf("channelRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatPeriod(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * 24 * time.Hour: "30d", 24 * time.Hour: "1d", 36 * time.Hour: "36h0m0s", 30 * time.Second: "30s",
	} {
		if got := formatPeriod(d); got != want {
			t.Errorf("formatPeriod(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestQuotaBeforeFirstUse(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{}, boards{}, nil, &quota{limit: 200, period: 30 * 24 * time.Hour}))
	assertContains(t, out, "*200* propsies/dropsies available (it resets 30d after you first use one).")
}

func TestLimitMessages(t *testing.T) {
	reset := time.Unix(1700000000, 0)
	slackReset := "<!date^1700000000^{date_short_pretty} at {time}|2023-11-14 22:13 UTC>"
	tests := []struct {
		name string
		err  *store.LimitError
		n    int
		want []string
	}{
		{
			"none left", &store.LimitError{Kind: store.LimitOverall, Limit: 200, Period: time.Hour, ResetsAt: reset},
			1,
			[]string{"used all 200 of your propsies/dropsies", "Your allowance resets " + slackReset},
		},
		{
			"some left", &store.LimitError{Kind: store.LimitOverall, Limit: 200, Left: 2, ResetsAt: reset},
			3,
			[]string{"That takes 3, but you only have 2 propsies/dropsies left", "resets"},
		},
		{
			"too many for an unopened window", &store.LimitError{Kind: store.LimitOverall, Limit: 2, Left: 2},
			3,
			[]string{"only have 2 propsies/dropsies left."},
		},
		{
			"per person", &store.LimitError{Kind: store.LimitPerPerson, Limit: 5, Period: 24 * time.Hour, Capped: []store.Capped{{UserID: "UB", ResetsAt: reset}}},
			1,
			[]string{"per-person limit (5 propsies/dropsies per 1d)", "<@UB> (resets " + slackReset + ")"},
		},
	}
	for _, tt := range tests {
		got := limitMessage(tt.err, tt.n)
		for _, want := range tt.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q missing %q", tt.name, got, want)
			}
		}
	}
	if strings.Contains(limitMessage(tests[2].err, 3), "resets") {
		t.Errorf("no reset clause expected when no window is open")
	}
}

func TestEveryoneOnTheBoardsIsListed(t *testing.T) {
	out := render(t, homeBlocks(store.Stats{}, boards{
		received: []store.Entry{{UserID: "UA", Count: 2}, {UserID: "UB", Count: 0}, {UserID: "UC", Count: -3}},
		given:    []store.Entry{{UserID: "UD", Count: 4}, {UserID: "UE", Count: 0}},
	}, nil, nil))
	assertContains(t, out,
		":first_place_medal: <@UA> — 2", "2. <@UB> — 0", "3. <@UC> — -3", // no medals at zero or below
		":first_place_medal: <@UD> — 4", "2. <@UE> — 0")
}

func TestBoardTiesShareARank(t *testing.T) {
	out := render(t, boardBlocks("T", []store.Entry{
		{UserID: "UA", Count: 5}, {UserID: "UB", Count: 5}, {UserID: "UC", Count: 3}, {UserID: "UD", Count: 3}, {UserID: "UE", Count: 1},
	}, "none", 0))
	assertContains(t, out,
		":first_place_medal: <@UA> — 5", ":first_place_medal: <@UB> — 5",
		":third_place_medal: <@UC> — 3", ":third_place_medal: <@UD> — 3", "5. <@UE> — 1")
}

func TestEmptyBoards(t *testing.T) {
	assertContains(t, render(t, homeBlocks(store.Stats{}, boards{}, nil, nil)),
		"No propsies yet", "Nobody has given anything yet")
}

// A big workspace must be listed in full, split over sections that each fit
// Slack's text limit, and the whole view must stay within its block limit.
func TestBigBoardIsChunkedNotTruncated(t *testing.T) {
	var entries []store.Entry
	for i := 0; i < 900; i++ {
		entries = append(entries, store.Entry{UserID: fmt.Sprintf("U%010d", i), Count: 900 - i})
	}
	blocks := homeBlocks(store.Stats{}, boards{received: entries, given: entries}, nil, nil)
	if len(blocks) > 100 {
		t.Fatalf("home view has %d blocks, Slack allows 100", len(blocks))
	}
	out := render(t, blocks)
	for _, id := range []string{"U0000000000", "U0000000450", "U0000000899"} {
		if strings.Count(out, "<@"+id+">") != 2 { // once per board
			t.Errorf("%s should be on both boards", id)
		}
	}
	for _, b := range blocks {
		if sec, ok := b.(*slack.SectionBlock); ok && len(sec.Text.Text) > 3000 {
			t.Errorf("section is %d chars, over Slack's 3000 limit", len(sec.Text.Text))
		}
	}
	if strings.Contains(out, "Showing the top") {
		t.Errorf("900 rows is under the cap; no truncation note expected")
	}
}

func TestBoardSaysWhenItWasCut(t *testing.T) {
	entries := []store.Entry{{UserID: "UA", Count: 3}, {UserID: "UB", Count: 2}}
	assertContains(t, render(t, boardBlocks("T", entries, "none", 2)), "Showing the top 2.")
	if strings.Contains(render(t, boardBlocks("T", entries, "none", 0)), "Showing the top") {
		t.Error("a complete board must not claim to be cut")
	}
}
