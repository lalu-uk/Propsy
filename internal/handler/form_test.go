package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	reason := o.view.Blocks.BlockSet[2].(*slack.InputBlock)
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
	reason := fs.opened[0].view.Blocks.BlockSet[2].(*slack.InputBlock)
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

func TestCountTokenGivesEachRecipientThatMany(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	ctx := context.Background()
	if err := h.HandleSlashCommand(ctx, propsy("UGIVER", "3 <@UA|a> <@UB|b> for all the things")); err != nil {
		t.Fatalf("HandleSlashCommand: %v", err)
	}
	for _, u := range []string{"UA", "UB"} {
		if st, _ := repo.Stats(ctx, u); st.PropsiesReceived != 3 {
			t.Errorf("%s received %d, want 3", u, st.PropsiesReceived)
		}
	}
	if got := fs.lastResponse(t).Text; !strings.Contains(got, "3 propsies") || !strings.Contains(got, "all the things") {
		t.Errorf("announcement = %q", got)
	}
}

func TestCountOutOfRangeIsRefused(t *testing.T) {
	for _, text := range []string{"0 <@UA>", "6 <@UA>"} {
		h, fs, repo := newTestHandler(t)
		if err := h.HandleSlashCommand(context.Background(), propsy("UGIVER", text)); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if r := fs.lastResponse(t); r.ResponseType != slack.ResponseTypeEphemeral {
			t.Errorf("%q: want an ephemeral refusal, got %+v", text, r)
		}
		if st, _ := repo.Stats(context.Background(), "UA"); st.PropsiesReceived != 0 {
			t.Errorf("%q: nothing should be recorded", text)
		}
	}
}

func TestCountCountsAgainstThePerPersonLimit(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	h.Settings = Settings{PerPersonLimit: 4, PerPersonLimitPeriod: 24 * time.Hour}
	ctx := context.Background()
	_ = h.HandleSlashCommand(ctx, propsy("UGIVER", "3 <@UA>"))
	if err := h.HandleSlashCommand(ctx, propsy("UGIVER", "2 <@UA>")); err != nil {
		t.Fatalf("HandleSlashCommand: %v", err)
	}
	if r := fs.lastResponse(t); !strings.Contains(r.Text, "per-person limit") {
		t.Errorf("second batch should hit the per-person limit, got %q", r.Text)
	}
	if st, _ := repo.Stats(ctx, "UA"); st.PropsiesReceived != 3 {
		t.Errorf("UA received %d, want 3: the refused batch records nothing", st.PropsiesReceived)
	}
}

func TestFormSubmissionWithCount(t *testing.T) {
	h, _, repo := newTestHandler(t)
	ctx := context.Background()
	cb := formSubmission(formMeta{UserID: testGiver, Command: commandPropsy, ChannelID: "C1", ResponseURL: testResponseURL}, []string{"UA"}, "")
	cb.View.State.Values[formBlockCount] = map[string]slack.BlockAction{formActionCount: {Value: "4"}}
	if err := h.HandleViewSubmission(ctx, cb); err != nil {
		t.Fatalf("HandleViewSubmission: %v", err)
	}
	if st, _ := repo.Stats(ctx, "UA"); st.PropsiesReceived != 4 {
		t.Errorf("UA received %d, want 4", st.PropsiesReceived)
	}
}

func TestFormCountDefaultsToOneAndRejectsBadValues(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int
		ok   bool
	}{{"", 1, true}, {"1", 1, true}, {"5", 5, true}, {"0", 0, false}, {"6", 0, false}, {"two", 0, false}} {
		got, err := parseFormCount(tt.in)
		if (err == nil) != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("parseFormCount(%q) = %d, %v; want %d, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestPickingPeopleUpdatesTheFormWithAllowances(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	h.Settings = Settings{PerPersonLimit: 5, PerPersonLimitPeriod: 24 * time.Hour}
	ctx := context.Background()
	_ = h.HandleSlashCommand(ctx, propsy("UGIVER", "2 <@UA>"))

	cb := formSubmission(formMeta{UserID: testGiver, Command: commandPropsy, ChannelID: "C1", ResponseURL: testResponseURL}, []string{"UA", "UB"}, "")
	cb.Type = slack.InteractionTypeBlockActions
	cb.View.ID = "V1"
	if err := h.HandleBlockAction(ctx, cb); err != nil {
		t.Fatalf("HandleBlockAction: %v", err)
	}
	if len(fs.updated) != 1 || fs.updated[0].triggerID != "V1" {
		t.Fatalf("want one view update for V1, got %+v", fs.updated)
	}
	ctxBlock, ok := fs.updated[0].view.Blocks.BlockSet[len(fs.updated[0].view.Blocks.BlockSet)-1].(*slack.ContextBlock)
	if !ok {
		t.Fatalf("last block is not a context block: %+v", fs.updated[0].view.Blocks.BlockSet)
	}
	text := ctxBlock.ContextElements.Elements[0].(*slack.TextBlockObject).Text
	if !strings.Contains(text, "<@UA>: *3* of 5 left") || !strings.Contains(text, "<@UB>: *5* of 5 left") {
		t.Errorf("context = %q", text)
	}
}

func TestPeoplePickerSendsSelectionEvents(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	cmd := propsy("UA", "")
	cmd.TriggerID = testTrigger
	_ = h.HandleSlashCommand(context.Background(), cmd)
	who := fs.opened[0].view.Blocks.BlockSet[0].(*slack.InputBlock)
	if !who.DispatchAction {
		t.Error("the people picker needs dispatch_action, or Slack never sends the selection")
	}
}
