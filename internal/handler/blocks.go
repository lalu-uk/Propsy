package handler

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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

// propsies and dropsies render a count with the right noun: "1 propsy",
// "5 propsies", "0 dropsies".
func propsies(n int) string { return counted(n, "propsy", "propsies") }
func dropsies(n int) string { return counted(n, "dropsy", "dropsies") }

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
		return ":droplet: dropsy!"
	}
	return ":raised_hands: propsy!"
}

// announcementBlocks is the public announcement for a propsy (delta > 0) or a
// dropsy (delta < 0); each person named gets one.
func announcementBlocks(from string, to []string, reason string, delta int) []slack.Block {
	text := fmt.Sprintf(":raised_hands: <@%s> gave a propsy to %s", from, mentions(to))
	if delta < 0 {
		text = fmt.Sprintf(":droplet: <@%s> gave a dropsy to %s", from, mentions(to))
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
		avail := fmt.Sprintf("You have *%d* propsies/dropsies available", max(q.limit-q.used, 0))
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
		return fmt.Sprintf("You've hit the per-person limit (%d propsies/dropsies per %s) for %s. Try again then, or leave them out.",
			e.Limit, formatPeriod(e.Period), strings.Join(parts, ", "))
	}
	if e.Left <= 0 {
		return fmt.Sprintf("You've used all %d of your propsies/dropsies.%s", e.Limit, resetClause(e.ResetsAt))
	}
	return fmt.Sprintf("That takes %d, but you only have %d propsies/dropsies left.%s", n, e.Left, resetClause(e.ResetsAt))
}

// totals renders "5 propsies" or "5 propsies, 10 dropsies"; dropsies are left
// out when there are none, so teams not using /dropsy never see them.
func totals(p, d int) string {
	if d == 0 {
		return propsies(p)
	}
	return propsies(p) + ", " + dropsies(d)
}

var medals = []string{":first_place_medal:", ":second_place_medal:", ":third_place_medal:"}

// homeBlocks is the app home dashboard. q is nil when there are no limits.
func homeBlocks(stats store.Stats, board []store.Entry, recent []store.Action, q *quota) []slack.Block {
	summary := fmt.Sprintf("*Received:* %s\n*Given:* %s\nGive some with `/propsy @someone for being awesome`.",
		totals(stats.PropsiesReceived, stats.DropsiesReceived), totals(stats.PropsiesGiven, stats.DropsiesGiven))
	if q != nil {
		summary += "\n" + strings.Join(q.lines(), "\n")
	}
	blocks := []slack.Block{
		slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, "Propsy :raised_hands:", true, false)),
		section(summary),
		slack.NewDividerBlock(),
		section("*Leaderboard*"),
	}

	if len(board) == 0 {
		blocks = append(blocks, section("_No propsies yet — be the first!_"))
	} else {
		var b strings.Builder
		for i, e := range board {
			rank := fmt.Sprintf("%d.", i+1)
			if i < len(medals) && e.Score > 0 {
				rank = medals[i]
			}
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "%s <@%s> — %d", rank, e.UserID, e.Score)
		}
		blocks = append(blocks, section(b.String()))
	}

	if len(recent) > 0 {
		blocks = append(blocks, slack.NewDividerBlock(), section("*Recently received*"))
		var b strings.Builder
		for i, a := range recent {
			if i > 0 {
				b.WriteByte('\n')
			}
			if a.Delta < 0 {
				fmt.Fprintf(&b, "• :droplet: <@%s> gave you a dropsy", a.From)
			} else {
				fmt.Fprintf(&b, "• <@%s> gave you a propsy", a.From)
			}
			if a.Reason != "" {
				fmt.Fprintf(&b, ": %s", a.Reason)
			}
			fmt.Fprintf(&b, " _(<!date^%d^{date_short_pretty}|%s>)_", a.CreatedAt.Unix(), a.CreatedAt.Format("2006-01-02"))
		}
		blocks = append(blocks, section(b.String()))
	}
	return blocks
}
