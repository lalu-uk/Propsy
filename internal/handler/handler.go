// Package handler contains the transport-agnostic Propsy logic: giving
// propsies (/propsy) and dropsies (/dropsy) via slash commands, and publishing
// the app home dashboard. The same handler serves both Socket Mode and HTTP
// webhook transports.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/laluowen/Propsy/internal/store"
	"github.com/slack-go/slack"
)

const (
	maxRecipients  = 10
	maxReasonRunes = 500
	recentCount    = 5
)

// SlackAPI is the subset of Slack the handler needs. Defining it here keeps
// the handler testable with fakes; Slack satisfies it.
type SlackAPI interface {
	PublishView(userID string, view slack.HomeTabViewRequest, hash string) (*slack.ViewResponse, error)
	PostMessage(channelID string, options ...slack.MsgOption) (string, string, error)
	// OpenView opens a modal from a slash command's trigger_id, which Slack
	// only accepts for about 3 seconds.
	OpenView(triggerID string, view slack.ModalViewRequest) (*slack.ViewResponse, error)
	// Respond posts to a slash command's response_url, which works in any
	// channel whether or not the bot has been invited to it.
	Respond(ctx context.Context, responseURL string, msg *slack.WebhookMessage) error
}

// Slack adapts a *slack.Client to SlackAPI.
type Slack struct{ *slack.Client }

// Respond implements SlackAPI.
func (s Slack) Respond(ctx context.Context, responseURL string, msg *slack.WebhookMessage) error {
	return slack.PostWebhookContext(ctx, responseURL, msg)
}

// Settings are the handler's tunables. A zero limit means unlimited.
type Settings struct {
	// Limit caps how many propsies plus dropsies one user may give per
	// LimitPeriod. The window opens when they first give one and resets in
	// full once the period has elapsed (see store.Limits).
	Limit       int
	LimitPeriod time.Duration

	// PerPersonLimit caps how many propsies plus dropsies one user may give
	// one other person per PerPersonLimitPeriod, with the same window rules.
	PerPersonLimit       int
	PerPersonLimitPeriod time.Duration

	// AnnounceChannel, if set, is where announcements are posted instead of
	// the channel the command was run in (a channel ID, or #name for a
	// public channel). The bot must be a member. If posting there fails, the
	// announcement falls back to the invoking channel.
	AnnounceChannel string

	// LeaderboardMax caps how many people each leaderboard lists. Zero lists
	// everyone, up to boardHardMax.
	LeaderboardMax int
}

// boardLimit is how many rows each leaderboard may show.
func (s Settings) boardLimit() int {
	if s.LeaderboardMax <= 0 || s.LeaderboardMax > boardHardMax {
		return boardHardMax
	}
	return s.LeaderboardMax
}

// Handler processes Propsy logic independent of transport.
type Handler struct {
	Slack SlackAPI
	Store store.Repository
	Now   func() time.Time
	Settings
}

// New builds a Handler with no limits and no announce channel; set Settings
// to change that.
func New(s SlackAPI, repo store.Repository) *Handler {
	return &Handler{Slack: s, Store: repo, Now: time.Now}
}

// Slack's escaped mention formats: <@U123> and <@U123|name>. Requires the
// slash command's "Escape channels, users, and links" setting.
var mentionRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)

const (
	commandPropsy = "/propsy"
	commandDropsy = "/dropsy"

	usagePropsy = "Usage: `/propsy @someone [@another …] for that thing`"
	usageDropsy = "Usage: `/dropsy @someone [@another …] for that thing you did`"
)

// HandleSlashCommand dispatches `/propsy @user [@user …] [reason]` (give a
// propsy) and `/dropsy @user [@user …] [reason]` (give a dropsy). With no
// recipients it opens the give form instead. Any other command name is refused.
func (h *Handler) HandleSlashCommand(ctx context.Context, cmd slack.SlashCommand) error {
	delta, usage, ok := commandDelta(cmd.Command)
	if !ok {
		return h.ephemeral(ctx, cmd, fmt.Sprintf("Sorry, I only answer to `%s` and `%s`, not `%s`.", commandPropsy, commandDropsy, cmd.Command))
	}

	recipients, reason := parseCommand(cmd.Text)
	if len(recipients) == 0 {
		if err := h.openForm(ctx, cmd, delta); err != nil {
			slog.Warn("open give form; showing usage instead", "err", err)
			return h.ephemeral(ctx, cmd, h.withQuota(ctx, cmd.UserID, usage))
		}
		return nil
	}
	return h.give(ctx, cmd, delta, recipients, reason)
}

// commandDelta maps a command name to its delta and usage line.
func commandDelta(command string) (delta int, usage string, ok bool) {
	switch command {
	case commandPropsy:
		return 1, usagePropsy, true
	case commandDropsy:
		return -1, usageDropsy, true
	}
	return 0, "", false
}

// give records a propsy (delta 1) or dropsy (delta -1) from cmd.UserID to each
// recipient and announces it. Shared by the slash command and the form.
func (h *Handler) give(ctx context.Context, cmd slack.SlashCommand, delta int, recipients []string, reason string) error {
	var others []string
	for _, r := range recipients {
		if r != cmd.UserID {
			others = append(others, r)
		}
	}

	switch {
	case len(others) == 0:
		return h.ephemeral(ctx, cmd, "Nice try, but you can't do that to yourself. :wink:")
	case len(others) > maxRecipients:
		return h.ephemeral(ctx, cmd, fmt.Sprintf("That's a lot of people! Please pick at most %d at a time.", maxRecipients))
	}

	now := h.Now()
	actions := make([]store.Action, len(others))
	for i, to := range others {
		actions[i] = store.Action{From: cmd.UserID, To: to, Reason: reason, ChannelID: cmd.ChannelID, CreatedAt: now, Delta: delta}
	}
	if err := h.Store.AddActions(ctx, actions, h.limits(), now); err != nil {
		var limitErr *store.LimitError
		if errors.As(err, &limitErr) {
			return h.ephemeral(ctx, cmd, limitMessage(limitErr, len(others)))
		}
		_ = h.ephemeral(ctx, cmd, "Sorry, something went wrong recording that. Please try again.")
		return err
	}

	// Home tabs aren't refreshed here: a tab is rebuilt from the store whenever
	// it's opened, and doing it live would put Slack calls on the 3s ack path.
	return h.announce(ctx, cmd, others, reason, delta)
}

// announce posts the public announcement: to AnnounceChannel when configured
// (confirming to the giver privately), otherwise, or if that fails, to the
// channel the command was run in.
func (h *Handler) announce(ctx context.Context, cmd slack.SlashCommand, to []string, reason string, delta int) error {
	blocks := announcementBlocks(cmd.UserID, to, reason, delta)
	text := fmt.Sprintf("%s <@%s> gave one to %s.", announcementText(delta), cmd.UserID, mentions(to))
	if reason != "" {
		text += " Reason: " + reason
	}

	if ch := h.AnnounceChannel; ch != "" && ch != cmd.ChannelID {
		_, _, err := h.Slack.PostMessage(ch, slack.MsgOptionBlocks(blocks...), slack.MsgOptionText(text, false))
		if err == nil {
			return h.ephemeral(ctx, cmd, fmt.Sprintf(":white_check_mark: Posted in %s.", channelRef(ch)))
		}
		slog.Warn("post to announce channel; falling back to the current channel", "channel", ch, "err", err)
	}

	return h.Slack.Respond(ctx, cmd.ResponseURL, &slack.WebhookMessage{
		ResponseType: slack.ResponseTypeInChannel,
		Blocks:       &slack.Blocks{BlockSet: blocks},
		Text:         text,
	})
}

// Ready reports whether the app can serve requests: the store is reachable.
// It backs the readiness probe, not liveness.
func (h *Handler) Ready(ctx context.Context) error {
	return h.Store.Ping(ctx)
}

// PublishHome renders and publishes the app home dashboard for userID. It's
// called when they open the Home tab.
func (h *Handler) PublishHome(ctx context.Context, userID string) error {
	stats, err := h.Store.Stats(ctx, userID)
	if err != nil {
		return err
	}
	b, err := h.loadBoards(ctx)
	if err != nil {
		return err
	}
	recent, err := h.Store.Recent(ctx, userID, recentCount)
	if err != nil {
		return err
	}
	q, err := h.quota(ctx, userID)
	if err != nil {
		return err
	}
	view := slack.HomeTabViewRequest{
		Type:   slack.VTHomeTab,
		Blocks: slack.Blocks{BlockSet: homeBlocks(stats, b, recent, q)},
	}
	if _, err := h.Slack.PublishView(userID, view, ""); err != nil {
		return fmt.Errorf("publish home for %s: %w", userID, err)
	}
	return nil
}

// loadBoards reads both leaderboards, up to boardLimit rows each (the stores
// return them highest first, ties by user ID). It asks for one row more than it
// shows so it can tell a board that was cut from one that just
// happens to be full.
func (h *Handler) loadBoards(ctx context.Context) (boards, error) {
	limit := h.boardLimit()
	var b boards
	for _, bd := range []struct {
		board store.Board
		out   *[]store.Entry
		cut   *int
	}{{store.BoardReceived, &b.received, &b.receivedCut}, {store.BoardGiven, &b.given, &b.givenCut}} {
		entries, err := h.Store.Leaderboard(ctx, bd.board, limit+1)
		if err != nil {
			return boards{}, err
		}
		if len(entries) > limit {
			entries, *bd.cut = entries[:limit], limit
		}
		*bd.out = entries
	}
	return b, nil
}

func (h *Handler) limits() store.Limits {
	return store.Limits{
		Overall: h.Limit, OverallPeriod: h.LimitPeriod,
		PerPerson: h.PerPersonLimit, PerPersonPeriod: h.PerPersonLimitPeriod,
	}
}

// quota reports how much of userID's overall allowance is spent, or nil when
// no limit is configured.
func (h *Handler) quota(ctx context.Context, userID string) (*quota, error) {
	if h.Limit <= 0 && h.PerPersonLimit <= 0 {
		return nil, nil
	}
	q := &quota{
		limit: h.Limit, period: h.LimitPeriod,
		perPerson: h.PerPersonLimit, perPersonPeriod: h.PerPersonLimitPeriod,
	}
	if h.Limit > 0 {
		al, err := h.Store.Allowance(ctx, userID, h.LimitPeriod, h.Now())
		if err != nil {
			return nil, err
		}
		q.used, q.resetsAt = al.Used, al.ResetsAt
	}
	return q, nil
}

// withQuota appends the user's remaining allowance to msg; if that can't be
// looked up it returns msg unchanged, since it's only a courtesy.
func (h *Handler) withQuota(ctx context.Context, userID, msg string) string {
	q, err := h.quota(ctx, userID)
	if err != nil {
		slog.Warn("look up quota", "user", userID, "err", err)
		return msg
	}
	if q == nil {
		return msg
	}
	return msg + "\n" + strings.Join(q.lines(), "\n")
}

func (h *Handler) ephemeral(ctx context.Context, cmd slack.SlashCommand, text string) error {
	return h.Slack.Respond(ctx, cmd.ResponseURL, &slack.WebhookMessage{
		ResponseType: slack.ResponseTypeEphemeral,
		Text:         text,
	})
}

// parseCommand extracts the unique mentioned user IDs (in order) and the
// remaining text as the reason.
func parseCommand(text string) (users []string, reason string) {
	seen := map[string]bool{}
	for _, m := range mentionRE.FindAllStringSubmatch(text, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			users = append(users, m[1])
		}
	}
	reason = strings.TrimSpace(mentionRE.ReplaceAllString(text, ""))
	reason = strings.TrimSpace(strings.TrimPrefix(reason, "for "))
	if r := []rune(reason); len(r) > maxReasonRunes {
		reason = string(r[:maxReasonRunes])
	}
	return users, reason
}
