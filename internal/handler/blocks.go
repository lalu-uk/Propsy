package handler

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/laluowen/Propsy/internal/config"
	"github.com/laluowen/Propsy/internal/store"
	"github.com/slack-go/slack"
)

func mrkdwn(text string) *slack.TextBlockObject {
	return slack.NewTextBlockObject(slack.MarkdownType, text, false, false)
}

func section(text string) slack.Block {
	return slack.NewSectionBlock(mrkdwn(text), nil, nil)
}

func mentions(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "<@" + id + ">"
	}
	return strings.Join(parts, ", ")
}

// Propsies and Dropsies render a count with the right noun: "1 Propsy",
// "5 Propsies", "0 Dropsies".
func propsies(n int) string { return counted(n, "Propsy", "Propsies") }
func dropsies(n int) string { return counted(n, "Dropsy", "Dropsies") }

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

var channelIDRE = regexp.MustCompile(`^[CG][A-Z0-9]{2,}$`)

// channelRef renders a configured channel (ID or name) as Slack text.
func channelRef(ch string) string {
	if channelIDRE.MatchString(ch) {
		return "<#" + ch + ">"
	}
	return "#" + strings.TrimPrefix(ch, "#")
}

func announcementText(delta int) string {
	if delta < 0 {
		return ":droplet: Dropsy!"
	}
	return ":raised_hands: Propsy!"
}

// giftNoun is "a Propsy" or "5 Dropsies" for the announcement.
func giftNoun(delta, count int) string {
	if delta < 0 {
		if count == 1 {
			return "a Dropsy"
		}
		return dropsies(count)
	}
	if count == 1 {
		return "a Propsy"
	}
	return propsies(count)
}

// announcementBlocks is the public announcement for a Propsy (delta > 0) or a
// Dropsy (delta < 0), count of them to each person named.
func announcementBlocks(from string, to []string, reason string, delta, count int) []slack.Block {
	text := fmt.Sprintf(":raised_hands: <@%s> gave %s to %s", from, giftNoun(delta, count), mentions(to))
	if delta < 0 {
		text = fmt.Sprintf(":droplet: <@%s> gave %s to %s", from, giftNoun(delta, count), mentions(to))
	}
	if reason != "" {
		text += "\n>" + strings.ReplaceAll(reason, "\n", "\n>")
	}
	return []slack.Block{section(text)}
}

// quota is how much of a user's rate limits is spent. A zero limit means that
// limit is off. resetsAt is when the overall allowance next resets to full; it's
// zero while no window is open.
type quota struct {
	used, limit     int
	period          time.Duration
	resetsAt        time.Time
	perPerson       int
	perPersonPeriod time.Duration
}

// slackTime renders t in each reader's own timezone.
func slackTime(t time.Time) string {
	return fmt.Sprintf("<!date^%d^{date_short_pretty} at {time}|%s>", t.Unix(), t.UTC().Format("2006-01-02 15:04 UTC"))
}

// lines describes the allowance in user-facing text.
func (q *quota) lines() []string {
	var out []string
	if q.limit > 0 {
		avail := fmt.Sprintf("You have *%d* Propsies/Dropsies available", max(q.limit-q.used, 0))
		if q.resetsAt.IsZero() {
			out = append(out, fmt.Sprintf("%s (it resets %s after you first use one).", avail, formatPeriod(q.period)))
		} else {
			out = append(out, fmt.Sprintf("%s, resetting to %d %s.", avail, q.limit, slackTime(q.resetsAt)))
		}
	}
	if q.perPerson > 0 {
		out = append(out, fmt.Sprintf("You can give any one person up to %d per %s.", q.perPerson, formatPeriod(q.perPersonPeriod)))
	}
	return out
}

// formatPeriod renders d the way it's configured: whole days as "30d",
// otherwise Go's duration syntax.
func formatPeriod(d time.Duration) string {
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

// resetClause is " Your allowance resets <time>." or "" when no window is open.
func resetClause(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return " Your allowance resets " + slackTime(t) + "."
}

// limitMessage explains, to the giver, why a command of n people was refused.
func limitMessage(e *store.LimitError, n int) string {
	if e.Kind == store.LimitPerPerson {
		parts := make([]string, len(e.Capped))
		for i, c := range e.Capped {
			parts[i] = fmt.Sprintf("<@%s> (resets %s)", c.UserID, slackTime(c.ResetsAt))
		}
		return fmt.Sprintf("You've hit the per-person limit (%d Propsies/Dropsies per %s) for %s. Try again then, or leave them out.",
			e.Limit, formatPeriod(e.Period), strings.Join(parts, ", "))
	}
	if e.Left <= 0 {
		return fmt.Sprintf("You've used all %d of your Propsies/Dropsies.%s", e.Limit, resetClause(e.ResetsAt))
	}
	return fmt.Sprintf("That takes %d, but you only have %d Propsies/Dropsies left.%s", n, e.Left, resetClause(e.ResetsAt))
}

// totals renders "5 Propsies" or "5 Propsies, 10 Dropsies"; Dropsies are left
// out when there are none, so teams not using /dropsy never see them.
func totals(p, d int) string {
	if d == 0 {
		return propsies(p)
	}
	return propsies(p) + ", " + dropsies(d)
}

var medals = []string{":first_place_medal:", ":second_place_medal:", ":third_place_medal:"}

const (
	// boardHardMax is the most rows a leaderboard ever shows, whatever is
	// configured (config clamps the setting to this and warns; this is the
	// backstop for Settings built directly), so a huge workspace can't push the
	// Home tab past Slack's 100-blocks-per-view limit.
	boardHardMax = config.MaxLeaderboardUsers
	// maxSectionChars keeps each section well under Slack's 3000-char limit.
	maxSectionChars = 2800
)

// boards are the two leaderboards, each sorted highest first. A cut field is
// the row limit a board was truncated to, or 0 if it's complete.
type boards struct {
	received    []store.Entry // everyone who has received a Propsy or Dropsy, by score
	receivedCut int
	given       []store.Entry // everyone who has given one, by how many (Propsies + Dropsies)
	givenCut    int
}

// boardBlocks renders a titled leaderboard of everyone in entries, split over
// as many sections as Slack's size limit needs. Equal counts share a rank, and
// the top three ranks with a positive count get medals. cut, if positive, is
// the limit the board was truncated to, which the board then says.
func boardBlocks(title string, entries []store.Entry, empty string, cut int) []slack.Block {
	blocks := []slack.Block{section("*" + title + "*")}
	if len(entries) == 0 {
		return append(blocks, section("_"+empty+"_"))
	}

	var chunk strings.Builder
	flush := func() {
		if chunk.Len() > 0 {
			blocks = append(blocks, section(chunk.String()))
			chunk.Reset()
		}
	}
	rank, prev := 0, 0
	for i, e := range entries {
		if i == 0 || e.Count != prev {
			rank = i + 1
		}
		prev = e.Count
		label := fmt.Sprintf("%d.", rank)
		if rank <= len(medals) && e.Count > 0 {
			label = medals[rank-1]
		}
		line := fmt.Sprintf("%s <@%s> — %d", label, e.UserID, e.Count)
		if chunk.Len() > 0 && chunk.Len()+1+len(line) > maxSectionChars {
			flush()
		}
		if chunk.Len() > 0 {
			chunk.WriteByte('\n')
		}
		chunk.WriteString(line)
	}
	flush()
	if cut > 0 {
		blocks = append(blocks, section(fmt.Sprintf("_Showing the top %d._", cut)))
	}
	return blocks
}

// homeBlocks is the app home dashboard. q is nil when there are no limits.
func homeBlocks(stats store.Stats, b boards, recent []store.Action, q *quota) []slack.Block {
	summary := fmt.Sprintf("*Received:* %s\n*Given:* %s\nGive some with `/propsy 2 @someone for that thing`.",
		totals(stats.PropsiesReceived, stats.DropsiesReceived), totals(stats.PropsiesGiven, stats.DropsiesGiven))
	if q != nil {
		summary += "\n" + strings.Join(q.lines(), "\n")
	}
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Propsy :raised_hands:", true, false)),
		section(summary),
		slack.NewDividerBlock(),
	}
	blocks = append(blocks, boardBlocks("Leaderboard: received", b.received, "No Propsies yet — be the first!", b.receivedCut)...)
	blocks = append(blocks, slack.NewDividerBlock())
	blocks = append(blocks, boardBlocks("Leaderboard: given", b.given, "Nobody has given anything yet.", b.givenCut)...)

	if groups := groupRecent(recent, recentCount); len(groups) > 0 {
		blocks = append(blocks, slack.NewDividerBlock(), section("*Recently received*"))
		for _, g := range groups {
			var b strings.Builder
			if g.Delta < 0 {
				fmt.Fprintf(&b, "• :droplet: <@%s> gave you %s", g.From, giftNoun(g.Delta, g.count))
			} else {
				fmt.Fprintf(&b, "• <@%s> gave you %s", g.From, giftNoun(g.Delta, g.count))
			}
			if g.Reason != "" {
				fmt.Fprintf(&b, ": %s", g.Reason)
			}
			fmt.Fprintf(&b, " _(<!date^%d^{date_short_pretty}|%s>)_", g.CreatedAt.Unix(), g.CreatedAt.Format("2006-01-02"))
			blocks = append(blocks, section(b.String()))
		}
	}
	return blocks
}

// recentGroup is one give as the recent list shows it: the Propsies or Dropsies
// one giver sent one person in one batch, which the store keeps as one row each.
type recentGroup struct {
	store.Action
	count int
}

// groupRecent folds consecutive rows of the same batch (same giver, reason,
// kind and time) into one group, newest first, and keeps at most limit groups.
func groupRecent(rows []store.Action, limit int) []recentGroup {
	var out []recentGroup
	for _, r := range rows {
		if n := len(out); n > 0 && sameBatch(out[n-1].Action, r) {
			out[n-1].count++
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, recentGroup{Action: r, count: 1})
	}
	return out
}

func sameBatch(a, b store.Action) bool {
	return a.From == b.From && a.Delta == b.Delta && a.Reason == b.Reason && a.CreatedAt.Equal(b.CreatedAt)
}
