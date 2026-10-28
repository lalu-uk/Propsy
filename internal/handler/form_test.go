package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

const (
	testResponseURL = "http://resp"
	testTrigger     = "TRIG"
	testGiver       = "UGIVER"
)

func formSubmission(meta formMeta, users []string, reason string) slack.InteractionCallback {
	m, _ := json.Marshal(meta)
	return slack.InteractionCallback{
		Type: slack.InteractionTypeViewSubmission,
		User: slack.User{ID: testGiver},
		View: slack.View{
			CallbackID:      formCallbackID,
			PrivateMetadata: string(m),
			State: &slack.ViewState{Values: map[string]map[string]slack.BlockAction{
				formBlockUsers:  {formActionUsers: {SelectedUsers: users}},
				formBlockReason: {formActionReason: {Value: reason}},
			}},
		},
	}
}

func TestNoRecipientsOpensTheForm(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	cmd := propsy("UA", "")
	cmd.TriggerID = testTrigger
	if err := h.HandleSlashCommand(context.Background(), cmd); err != nil {
		t.Fatalf("HandleSlashCommand: %v", err)
	}
	if len(fs.opened) != 1 || len(fs.responses) != 0 {
		t.Fatalf("want one opened view and no response, got %d views, %d responses", len(fs.opened), len(fs.responses))
	}
	o := fs.opened[0]
	if o.triggerID != testTrigger || o.view.CallbackID != formCallbackID || o.view.Type != slack.VTModal {
		t.Errorf("opened %+v", o)
	}
	var meta formMeta
	if err := json.Unmarshal([]byte(o.view.PrivateMetadata), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Command != commandPropsy || meta.ResponseURL != testResponseURL || meta.ChannelID != "C1" {
		t.Errorf("metadata = %+v", meta)
	}
	reason := o.view.Blocks.BlockSet[1].(*slack.InputBlock)
	if reason.Optional != true || reason.Element.(*slack.PlainTextInputBlockElement).Placeholder.Text != "for that thing" {
		t.Errorf("reason field = %+v", reason)
	}
}

func TestDropsyFormHasItsOwnTitleAndHint(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	cmd := dropsy("UA", "")
	cmd.TriggerID = testTrigger
	_ = h.HandleSlashCommand(context.Background(), cmd)
	if got := fs.opened[0].view.Title.Text; got != "Give a dropsy" {
		t.Errorf("title = %q", got)
	}
	reason := fs.opened[0].view.Blocks.BlockSet[1].(*slack.InputBlock)
	if got := reason.Element.(*slack.PlainTextInputBlockElement).Placeholder.Text; got != "for that thing you did" {
		t.Errorf("placeholder = %q", got)
	}
}

func TestFormSubmissionGivesPropsies(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	ctx := context.Background()
	cb := formSubmission(formMeta{Command: commandPropsy, ChannelID: "C1", ResponseURL: testResponseURL}, []string{"UA", "UB"}, "for shipping")
	if err := h.HandleViewSubmission(ctx, cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	if st, _ := repo.Stats(ctx, "UA"); st.PropsiesReceived != 1 {
		t.Errorf("UA stats = %+v", st)
	}
	if got := fs.lastResponse(t).Text; !strings.Contains(got, "for shipping") {
		t.Errorf("announcement = %q", got)
	}
}

func TestFormSubmissionDropsyAndSelfSkipped(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	ctx := context.Background()
	cb := formSubmission(formMeta{Command: commandDropsy, ChannelID: "C1", ResponseURL: testResponseURL}, []string{"UGIVER", "UA"}, "")
	if err := h.HandleViewSubmission(ctx, cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	if st, _ := repo.Stats(ctx, "UA"); st.DropsiesReceived != 1 || st.PropsiesReceived != 0 {
		t.Errorf("UA stats = %+v, want one dropsy", st)
	}
	if got := fs.lastResponse(t).Text; !strings.Contains(got, "dropsy") && !strings.Contains(got, "Dropsy") {
		t.Errorf("announcement = %q", got)
	}
}

func TestFormSubmissionWithNobodyAsksAgain(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	cb := formSubmission(formMeta{Command: commandPropsy, ChannelID: "C1", ResponseURL: testResponseURL}, nil, "")
	if err := h.HandleViewSubmission(context.Background(), cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	if got := fs.lastResponse(t).Text; !strings.Contains(got, "Pick at least one") {
		t.Errorf("response = %q", got)
	}
}

func TestFormSubmissionCapsTheReason(t *testing.T) {
	h, _, repo := newTestHandler(t)
	ctx := context.Background()
	long := strings.Repeat("x", maxReasonRunes+50)
	cb := formSubmission(formMeta{Command: commandPropsy, ChannelID: "C1", ResponseURL: testResponseURL}, []string{"UA"}, long)
	if err := h.HandleViewSubmission(ctx, cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	entries, _ := repo.Recent(ctx, "UA", 1)
	if len(entries) != 1 || len([]rune(entries[0].Reason)) != maxReasonRunes {
		t.Errorf("stored reason length wrong: %+v", entries)
	}
}

func TestOtherViewSubmissionsAreIgnored(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	cb := formSubmission(formMeta{Command: commandPropsy}, []string{"UA"}, "")
	cb.View.CallbackID = "something_else"
	if err := h.HandleViewSubmission(context.Background(), cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	if len(fs.responses) != 0 {
		t.Errorf("unexpected response for unrelated view")
	}
}
