package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

// The give form is the modal opened by a slash command with no recipients.
// Its block and action IDs are what Slack echoes back on submission and on
// selection changes.
const (
	formCallbackID   = "give_form"
	formBlockUsers   = "recipients"
	formActionUsers  = "recipients"
	formBlockReason  = "reason"
	formActionReason = "reason"
	formBlockCount   = "count"
	formActionCount  = "count"
	formContextBlock = "context"
)

// formMeta travels in the modal's private_metadata: who is giving, the command
// it came from (which fixes propsy or dropsy), and where to send replies.
type formMeta struct {
	UserID      string `json:"user_id"`
	Command     string `json:"command"`
	ChannelID   string `json:"channel_id"`
	ResponseURL string `json:"response_url"`
}

// formValues is what the user has entered in the form so far.
type formValues struct {
	users  []string
	reason string
	count  string
}

// openForm opens the give form for cmd. The trigger_id must be used within
// about 3 seconds, so this does nothing else before calling Slack.
func (h *Handler) openForm(ctx context.Context, cmd slack.SlashCommand, delta int) error {
	meta := formMeta{UserID: cmd.UserID, Command: cmd.Command, ChannelID: cmd.ChannelID, ResponseURL: cmd.ResponseURL}
	view, err := h.formView(ctx, meta, delta, formValues{})
	if err != nil {
		slog.Warn("build give form context", "user", cmd.UserID, "err", err)
	}
	_, err = h.Slack.OpenView(cmd.TriggerID, view)
	return err
}

// formView builds the give form with the values entered so far. Its context
// block shows the giver's allowance and, once people are picked, how much of
// the per-person limit each of them has left.
func (h *Handler) formView(ctx context.Context, meta formMeta, delta int, v formValues) (slack.ModalViewRequest, error) {
	title, hint := "Give a propsy", "for that thing"
	if delta < 0 {
		title, hint = "Give a dropsy", "for that thing you did"
	}

	who := slack.NewInputBlock(formBlockUsers, plainText("Who?"), nil,
		slack.NewOptionsMultiSelectBlockElement(slack.MultiOptTypeUser, plainText("Pick people"), formActionUsers).
			WithInitialUsers(v.users...))
	count := slack.NewInputBlock(formBlockCount, plainText("How many each? (1 to 5)"), nil,
		slack.NewPlainTextInputBlockElement(plainText("1"), formActionCount).WithInitialValue(v.count))
	count.Optional = true
	why := slack.NewInputBlock(formBlockReason, plainText("Why?"), nil,
		slack.NewPlainTextInputBlockElement(plainText(hint), formActionReason).WithInitialValue(v.reason))
	why.Optional = true

	blocks := []slack.Block{who, count, why}
	if lines := h.formContext(ctx, meta.UserID, v.users); len(lines) > 0 {
		blocks = append(blocks, slack.NewContextBlock(formContextBlock,
			slack.NewTextBlockObject("mrkdwn", strings.Join(lines, "\n"), false, false)))
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return slack.ModalViewRequest{}, err
	}
	return slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           plainText(title),
		Submit:          plainText("Send"),
		Close:           plainText("Cancel"),
		CallbackID:      formCallbackID,
		PrivateMetadata: string(raw),
		Blocks:          slack.Blocks{BlockSet: blocks},
	}, nil
}

// formContext is the context block's lines: the giver's overall allowance, then
// the per-person allowance for each person picked so far.
func (h *Handler) formContext(ctx context.Context, giver string, picked []string) []string {
	q, err := h.quota(ctx, giver)
	if err != nil {
		slog.Warn("look up quota for form", "user", giver, "err", err)
	}
	var lines []string
	if q != nil {
		lines = append(lines, q.lines()...)
	}
	if h.PerPersonLimit <= 0 {
		return lines
	}
	for _, to := range picked {
		al, err := h.Store.PairAllowance(ctx, giver, to, h.PerPersonLimitPeriod, h.Now())
		if err != nil {
			slog.Warn("look up per-person allowance for form", "user", giver, "to", to, "err", err)
			continue
		}
		left := max(h.PerPersonLimit-al.Used, 0)
		line := fmt.Sprintf("<@%s>: *%d* of %d left", to, left, h.PerPersonLimit)
		if al.Used > 0 {
			line += ", resets " + slackTime(al.ResetsAt)
		}
		lines = append(lines, line)
	}
	return lines
}

// formValuesFrom reads what the user has entered from a form's state.
func formValuesFrom(state *slack.ViewState) formValues {
	var v formValues
	if state == nil {
		return v
	}
	v.users = state.Values[formBlockUsers][formActionUsers].SelectedUsers
	v.reason = strings.TrimSpace(state.Values[formBlockReason][formActionReason].Value)
	v.count = strings.TrimSpace(state.Values[formBlockCount][formActionCount].Value)
	return v
}

// parseFormMeta reads the form's private metadata and the delta its command
// implies.
func parseFormMeta(raw string) (formMeta, int, error) {
	var meta formMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return formMeta{}, 0, fmt.Errorf("form metadata: %w", err)
	}
	delta, _, ok := commandDelta(meta.Command)
	if !ok {
		return formMeta{}, 0, fmt.Errorf("form for unknown command %q", meta.Command)
	}
	return meta, delta, nil
}

// HandleBlockAction refreshes the give form when the people picked change, so
// its context shows each person's remaining allowance. Other block actions are
// ignored.
func (h *Handler) HandleBlockAction(ctx context.Context, cb slack.InteractionCallback) error {
	if cb.View.CallbackID != formCallbackID {
		return nil
	}
	meta, delta, err := parseFormMeta(cb.View.PrivateMetadata)
	if err != nil {
		return err
	}
	view, err := h.formView(ctx, meta, delta, formValuesFrom(cb.View.State))
	if err != nil {
		return err
	}
	_, err = h.Slack.UpdateView(view, "", "", cb.View.ID)
	return err
}

// HandleViewSubmission records a give form's submission. Slack expects an empty
// acknowledgement, so this only does the work. Replies go to the response_url
// carried in the form's metadata.
func (h *Handler) HandleViewSubmission(ctx context.Context, cb slack.InteractionCallback) error {
	if cb.View.CallbackID != formCallbackID {
		return nil
	}
	meta, delta, err := parseFormMeta(cb.View.PrivateMetadata)
	if err != nil {
		return err
	}
	v := formValuesFrom(cb.View.State)
	cmd := slack.SlashCommand{Command: meta.Command, UserID: cb.User.ID, ChannelID: meta.ChannelID, ResponseURL: meta.ResponseURL}

	count, err := parseFormCount(v.count)
	if err != nil {
		return h.ephemeral(ctx, cmd, err.Error())
	}
	if len(v.users) == 0 {
		return h.ephemeral(ctx, cmd, "Pick at least one person to give one to.")
	}
	reason := v.reason
	if r := []rune(reason); len(r) > maxReasonRunes {
		reason = string(r[:maxReasonRunes])
	}
	return h.give(ctx, cmd, delta, v.users, reason, count)
}

// parseFormCount reads the form's count field: blank means one.
func parseFormCount(s string) (int, error) {
	if s == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxPerRecipient {
		return 0, fmt.Errorf("how many each? pick a whole number from 1 to %d", maxPerRecipient)
	}
	return n, nil
}

func plainText(s string) *slack.TextBlockObject {
	return slack.NewTextBlockObject("plain_text", s, false, false)
}
