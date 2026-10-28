package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/slack-go/slack"
)

// The give form is the modal opened by a slash command with no recipients.
// Its block and action IDs are what Slack echoes back on submission.
const (
	formCallbackID   = "give_form"
	formBlockUsers   = "recipients"
	formActionUsers  = "recipients"
	formBlockReason  = "reason"
	formActionReason = "reason"
)

// formMeta travels in the modal's private_metadata: the command it came from
// (which fixes propsy or dropsy), and where to send replies.
type formMeta struct {
	Command     string `json:"command"`
	ChannelID   string `json:"channel_id"`
	ResponseURL string `json:"response_url"`
}

// openForm opens the give form for cmd. The trigger_id must be used within
// about 3 seconds, so this does nothing else before calling Slack.
func (h *Handler) openForm(ctx context.Context, cmd slack.SlashCommand, delta int) error {
	meta, err := json.Marshal(formMeta{Command: cmd.Command, ChannelID: cmd.ChannelID, ResponseURL: cmd.ResponseURL})
	if err != nil {
		return err
	}

	title, hint := "Give a propsy", "for that thing"
	if delta < 0 {
		title, hint = "Give a dropsy", "for that thing you did"
	}

	who := slack.NewInputBlock(formBlockUsers, plainText("Who?"), nil,
		slack.NewOptionsMultiSelectBlockElement(slack.MultiOptTypeUser, plainText("Pick people"), formActionUsers))
	why := slack.NewInputBlock(formBlockReason, plainText("Why?"), nil,
		slack.NewPlainTextInputBlockElement(plainText(hint), formActionReason))
	why.Optional = true

	blocks := []slack.Block{who, why}
	q, err := h.quota(ctx, cmd.UserID)
	if err != nil {
		slog.Warn("look up quota for form", "user", cmd.UserID, "err", err)
	}
	if q != nil {
		blocks = append(blocks, slack.NewContextBlock("quota", slack.NewTextBlockObject("mrkdwn", strings.Join(q.lines(), "\n"), false, false)))
	}

	_, err = h.Slack.OpenView(cmd.TriggerID, slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           plainText(title),
		Submit:          plainText("Send"),
		Close:           plainText("Cancel"),
		CallbackID:      formCallbackID,
		PrivateMetadata: string(meta),
		Blocks:          slack.Blocks{BlockSet: blocks},
	})
	return err
}

// HandleViewSubmission records a give form's submission. Slack expects an empty
// acknowledgement, so this only does the work. Replies go to the response_url
// carried in the form's metadata.
func (h *Handler) HandleViewSubmission(ctx context.Context, cb slack.InteractionCallback) error {
	if cb.View.CallbackID != formCallbackID {
		return nil
	}
	var meta formMeta
	if err := json.Unmarshal([]byte(cb.View.PrivateMetadata), &meta); err != nil {
		return fmt.Errorf("form metadata: %w", err)
	}
	delta, _, ok := commandDelta(meta.Command)
	if !ok {
		return fmt.Errorf("form for unknown command %q", meta.Command)
	}

	var users []string
	var reason string
	if st := cb.View.State; st != nil {
		users = st.Values[formBlockUsers][formActionUsers].SelectedUsers
		reason = strings.TrimSpace(st.Values[formBlockReason][formActionReason].Value)
	}
	if r := []rune(reason); len(r) > maxReasonRunes {
		reason = string(r[:maxReasonRunes])
	}

	cmd := slack.SlashCommand{Command: meta.Command, UserID: cb.User.ID, ChannelID: meta.ChannelID, ResponseURL: meta.ResponseURL}
	if len(users) == 0 {
		return h.ephemeral(ctx, cmd, "Pick at least one person to give one to.")
	}
	return h.give(ctx, cmd, delta, users, reason)
}

func plainText(s string) *slack.TextBlockObject {
	return slack.NewTextBlockObject("plain_text", s, false, false)
}
