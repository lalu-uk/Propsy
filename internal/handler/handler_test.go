package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/laluowen/Propsy/internal/store"
	"github.com/slack-go/slack"
)

type posted struct {
	channel string
	text    string
}

type fakeSlack struct {
	responses []*slack.WebhookMessage
	posts     []posted
	postErr   error
	published map[string]slack.HomeTabViewRequest
}

func (f *fakeSlack) PublishView(userID string, v slack.HomeTabViewRequest, _ string) (*slack.ViewResponse, error) {
	if f.published == nil {
		f.published = map[string]slack.HomeTabViewRequest{}
	}
	f.published[userID] = v
	return &slack.ViewResponse{}, nil
}

func (f *fakeSlack) PostMessage(channelID string, opts ...slack.MsgOption) (string, string, error) {
	if f.postErr != nil {
		return "", "", f.postErr
	}
	_, vals, _ := slack.UnsafeApplyMsgOptions("tok", channelID, "https://slack.test/api/", opts...)
	f.posts = append(f.posts, posted{channel: channelID, text: vals.Get("text")})
	return channelID, "1.1", nil
}

func (f *fakeSlack) Respond(_ context.Context, _ string, m *slack.WebhookMessage) error {
	f.responses = append(f.responses, m)
	return nil
}

func (f *fakeSlack) lastResponse(t *testing.T) *slack.WebhookMessage {
	t.Helper()
	if len(f.responses) == 0 {
		t.Fatal("no response was sent")
	}
	return f.responses[len(f.responses)-1]
}

func newTestHandler(t *testing.T) (*Handler, *fakeSlack, store.Repository) {
	t.Helper()
	repo, err := store.NewJSONFile("")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	fs := &fakeSlack{}
	h := New(fs, repo)
	h.Now = func() time.Time { return time.Unix(1700000000, 0) }
	return h, fs, repo
}

const (
	kudosChannel = "CKUDOS"
	usageNeedle  = "/propsy @someone"
)

func propsy(user, text string) slack.SlashCommand {
	return slack.SlashCommand{Command: "/propsy", UserID: user, ChannelID: "C1", Text: text, ResponseURL: "http://resp"}
}

func dropsy(user, text string) slack.SlashCommand {
	c := propsy(user, text)
	c.Command = "/dropsy"
	return c
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		text       string
		wantUsers  []string
		wantReason string
	}{
		{"", nil, ""},
		{"just words", nil, "just words"},
		{"<@U1|alice> for the help", []string{"U1"}, "the help"},
		{"<@U1> <@W2|bob> great work", []string{"U1", "W2"}, "great work"},
		{"<@U1> <@U1> twice", []string{"U1"}, "twice"},
		{"thanks <@U1> for fixing prod", []string{"U1"}, "thanks  for fixing prod"},
		{"<@U1> " + strings.Repeat("x", 600), []string{"U1"}, strings.Repeat("x", maxReasonRunes)},
	}
	for _, tt := range tests {
		users, reason := parseCommand(tt.text)
		if strings.Join(users, ",") != strings.Join(tt.wantUsers, ",") || reason != tt.wantReason {
			t.Errorf("parseCommand(%.30q) = %v, %q; want %v, %q", tt.text, users, reason, tt.wantUsers, tt.wantReason)
		}
	}
}

func TestOnlyAnswersToPropsyAndDropsy(t *testing.T) {
	for _, name := range []string{"/props", "/kudos", ""} {
		h, fs, repo := newTestHandler(t)
		c := propsy("UA", "<@UB> hi")
		c.Command = name
		if err := h.HandleSlashCommand(context.Background(), c); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		r := fs.lastResponse(t)
		if r.ResponseType != slack.ResponseTypeEphemeral || !strings.Contains(r.Text, "/propsy") || !strings.Contains(r.Text, "/dropsy") {
			t.Errorf("%q: response = %+v, want ephemeral naming the two real commands", name, r)
		}
		if st, _ := repo.Stats(context.Background(), "UB"); st.PropsiesReceived != 0 {
			t.Errorf("%q: must not record anything", name)
		}
	}
}

func TestGivePropsies(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	ctx := context.Background()

	if err := h.HandleSlashCommand(ctx, propsy("UGIVER", "<@UA|a> <@UB|b> for shipping")); err != nil {
		t.Fatalf("HandleSlashCommand: %v", err)
	}

	for _, u := range []string{"UA", "UB"} {
		if st, _ := repo.Stats(ctx, u); st.PropsiesReceived != 1 || st.DropsiesReceived != 0 {
			t.Errorf("%s stats = %+v, want 1 propsy received", u, st)
		}
	}
	if st, _ := repo.Stats(ctx, "UGIVER"); st.PropsiesGiven != 2 || st.DropsiesGiven != 0 {
		t.Errorf("giver stats = %+v, want 2 propsies given", st)
	}
	if r := fs.lastResponse(t); len(fs.responses) != 1 || r.ResponseType != slack.ResponseTypeInChannel {
		t.Fatalf("want one in_channel response, got %+v", fs.responses)
	}
	if len(fs.published) != 0 {
		t.Errorf("giving must not publish Home tabs (they're built when opened), published %d", len(fs.published))
	}
}

func TestGiveDropsiesAreTrackedSeparately(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	ctx := context.Background()
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> thanks"))
	_ = h.HandleSlashCommand(ctx, propsy("UC", "<@UB> thanks"))
	fs.responses = nil

	if err := h.HandleSlashCommand(ctx, dropsy("UD", "<@UB> for being a menace")); err != nil {
		t.Fatalf("dropsy: %v", err)
	}
	st, _ := repo.Stats(ctx, "UB")
	if st.PropsiesReceived != 2 || st.DropsiesReceived != 1 || st.Score() != 1 {
		t.Errorf("UB stats = %+v, want 2 propsies, 1 dropsy received", st)
	}
	if st, _ := repo.Stats(ctx, "UD"); st.DropsiesGiven != 1 || st.PropsiesGiven != 0 {
		t.Errorf("UD stats = %+v, want 1 dropsy given", st)
	}
	if r := fs.lastResponse(t); r.ResponseType != slack.ResponseTypeInChannel || !strings.Contains(r.Text, "dropsy") {
		t.Errorf("want an in_channel dropsy announcement, got %+v", r)
	}
}

func TestRejections(t *testing.T) {
	tests := []struct {
		name string
		cmd  slack.SlashCommand
		want string
	}{
		{"propsy usage", propsy("UME", ""), usageNeedle},
		{"propsy no mention", propsy("UME", "alice"), usageNeedle},
		{"dropsy usage", dropsy("UME", ""), "/dropsy @someone"},
		{"self only", propsy("UME", "<@UME> me"), "yourself"},
		{"dropsy self only", dropsy("UME", "<@UME> me"), "yourself"},
		{"too many", propsy("UME", "<@U1><@U2><@U3><@U4><@U5><@U6><@U7><@U8><@U9><@UA><@UB>"), "at most"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fs, repo := newTestHandler(t)
			if err := h.HandleSlashCommand(context.Background(), tt.cmd); err != nil {
				t.Fatalf("err: %v", err)
			}
			r := fs.lastResponse(t)
			if len(fs.responses) != 1 || r.ResponseType != slack.ResponseTypeEphemeral || !strings.Contains(r.Text, tt.want) {
				t.Errorf("responses = %+v, want ephemeral containing %q", fs.responses, tt.want)
			}
			if board, _ := repo.Leaderboard(context.Background(), store.BoardReceived, 10); len(board) != 0 {
				t.Errorf("nothing should be recorded, got %+v", board)
			}
		})
	}
}

func TestSelfAmongOthersIsSkipped(t *testing.T) {
	h, _, repo := newTestHandler(t)
	if err := h.HandleSlashCommand(context.Background(), propsy("UME", "<@UME> <@UB> hi")); err != nil {
		t.Fatal(err)
	}
	if st, _ := repo.Stats(context.Background(), "UME"); st.PropsiesReceived != 0 || st.PropsiesGiven != 1 {
		t.Errorf("stats = %+v", st)
	}
}

func TestHomeListsEveryoneOnBothBoards(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	ctx := context.Background()
	// 15 distinct receivers: more than the old top 10.
	for i := 0; i < 15; i++ {
		_ = h.HandleSlashCommand(ctx, propsy("UGIVER", fmt.Sprintf("<@UREC%02d> hi", i)))
	}
	_ = h.HandleSlashCommand(ctx, dropsy("UMEAN", "<@UVICTIM> grr")) // dropsy-only on both sides
	if err := h.PublishHome(ctx, "UGIVER"); err != nil {
		t.Fatal(err)
	}
	out := render(t, fs.published["UGIVER"].Blocks.BlockSet)
	for i := 0; i < 15; i++ {
		if !strings.Contains(out, fmt.Sprintf("<@UREC%02d> — 1", i)) {
			t.Errorf("UREC%02d missing from the received board", i)
		}
	}
	for _, want := range []string{"<@UVICTIM> — -1", "<@UGIVER> — 15", "<@UMEAN> — 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestLeaderboardMax(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	ctx := context.Background()
	for i := 1; i <= 4; i++ { // UREC1 gets 1 propsy, UREC2 gets 2, ... so the order is UREC4, 3, 2, 1
		for j := 0; j < i; j++ {
			_ = h.HandleSlashCommand(ctx, propsy(fmt.Sprintf("UG%d%d", i, j), fmt.Sprintf("<@UREC%d> hi", i)))
		}
	}
	home := func() string {
		t.Helper()
		if err := h.PublishHome(ctx, "UX"); err != nil {
			t.Fatal(err)
		}
		return render(t, fs.published["UX"].Blocks.BlockSet)
	}

	h.LeaderboardMax = 2
	out := home()
	if !strings.Contains(out, "<@UREC4> — 4") || !strings.Contains(out, "<@UREC3> — 3") ||
		strings.Contains(out, "<@UREC2> —") || strings.Contains(out, "<@UREC1> —") {
		t.Errorf("max 2 should list only the top two of the received board: %s", out)
	}
	if strings.Count(out, "Showing the top 2.") != 2 { // received has 4 people, given has 10
		t.Errorf("both boards were cut and should say so: %s", out)
	}

	h.LeaderboardMax = 4 // exactly as many people as the received board has: not cut
	out = home()
	if !strings.Contains(out, "<@UREC1> — 1") || strings.Count(out, "Showing the top 4.") != 1 {
		t.Errorf("max 4 lists all 4 receivers without a cut note (only the 10-giver board is cut): %s", out)
	}

	for _, unlimited := range []int{0, -1} {
		h.LeaderboardMax = unlimited
		if out = home(); strings.Contains(out, "Showing the top") || !strings.Contains(out, "<@UREC1> — 1") {
			t.Errorf("LeaderboardMax=%d should list everyone: %s", unlimited, out)
		}
	}
}

func TestBoardLimitIsClampedToTheHardMax(t *testing.T) {
	for in, want := range map[int]int{0: boardHardMax, -5: boardHardMax, 10: 10, boardHardMax: boardHardMax, boardHardMax + 1: boardHardMax} {
		if got := (Settings{LeaderboardMax: in}).boardLimit(); got != want {
			t.Errorf("boardLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPublishHome(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	ctx := context.Background()
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> thanks"))
	if err := h.PublishHome(ctx, "UB"); err != nil {
		t.Fatalf("PublishHome: %v", err)
	}
	v := fs.published["UB"]
	if v.Type != slack.VTHomeTab || len(v.Blocks.BlockSet) == 0 {
		t.Fatalf("bad view: %+v", v)
	}
}

func TestOverallLimitResetsInFullAfterThePeriod(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	h.Limit, h.LimitPeriod = 3, 24*time.Hour
	ctx := context.Background()
	t0 := time.Unix(1700000000, 0)
	now := t0
	h.Now = func() time.Time { return now }

	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> <@UC> hi")) // opens the window at t0
	now = t0.Add(12 * time.Hour)
	_ = h.HandleSlashCommand(ctx, dropsy("UA", "<@UD> lol")) // dropsies spend the same allowance
	if fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Fatalf("first two commands should succeed: %+v", fs.responses)
	}

	now = t0.Add(13 * time.Hour)
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UE> one more"))
	r := fs.lastResponse(t)
	if r.ResponseType != slack.ResponseTypeEphemeral || !strings.Contains(r.Text, "used all 3") ||
		!strings.Contains(r.Text, "resets <!date^"+strconv.FormatInt(t0.Add(24*time.Hour).Unix(), 10)) {
		t.Errorf("over-limit message = %+v; want 'used all 3' and the reset time (t0+24h)", r)
	}
	if st, _ := repo.Stats(ctx, "UE"); st.PropsiesReceived != 0 {
		t.Errorf("rejected propsy must not be recorded")
	}

	// Other givers have their own allowance.
	_ = h.HandleSlashCommand(ctx, propsy("UB", "<@UA> hi"))
	if fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("another user should still be able to give propsies")
	}

	// Once the period since the window opened has elapsed it resets to the full
	// 3, not just the part that has "aged out": the dropsy given at +12h doesn't
	// count against the new window.
	now = t0.Add(24*time.Hour + time.Second)
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UE> <@UF> <@UG> back again"))
	if fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("allowance should be back to 3, got %+v", fs.lastResponse(t))
	}
}

func TestOverallLimitIsAllOrNothing(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	h.Limit, h.LimitPeriod = 2, time.Hour
	ctx := context.Background()
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> <@UC> <@UD> hi"))
	if got := fs.lastResponse(t).Text; !strings.Contains(got, "takes 3") || !strings.Contains(got, "only have 2 propsies/dropsies left") {
		t.Errorf("message = %q", got)
	}
	if board, _ := repo.Leaderboard(ctx, store.BoardReceived, 10); len(board) != 0 {
		t.Errorf("a command exceeding the allowance must record nothing, got %+v", board)
	}
}

func TestPerPersonLimit(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	h.PerPersonLimit, h.PerPersonLimitPeriod = 2, 24*time.Hour
	ctx := context.Background()
	t0 := time.Unix(1700000000, 0)
	now := t0
	h.Now = func() time.Time { return now }

	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> one"))
	_ = h.HandleSlashCommand(ctx, dropsy("UA", "<@UB> two")) // dropsies count towards the same cap
	if fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Fatalf("first two should succeed")
	}

	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> <@UC> three"))
	r := fs.lastResponse(t)
	if r.ResponseType != slack.ResponseTypeEphemeral || !strings.Contains(r.Text, "<@UB> (resets <!date^") || strings.Contains(r.Text, "<@UC>") ||
		!strings.Contains(r.Text, "2 propsies/dropsies per 1d") {
		t.Errorf("message = %+v; want ephemeral naming only the capped person and when they reset", r)
	}
	if st, _ := repo.Stats(ctx, "UC"); st.PropsiesReceived != 0 {
		t.Errorf("a command with a capped recipient must record nothing")
	}

	// Someone else, and other givers, are unaffected.
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UC> fine"))
	_ = h.HandleSlashCommand(ctx, propsy("UD", "<@UB> fine"))
	if len(fs.responses) != 5 || fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("responses = %+v", fs.responses)
	}

	now = t0.Add(24*time.Hour + time.Second)
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> next day"))
	if fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("per-person allowance should reset after the period")
	}
}

func TestUnlimitedByDefault(t *testing.T) {
	h, _, repo := newTestHandler(t)
	for i := 0; i < 8; i++ {
		_ = h.HandleSlashCommand(context.Background(), propsy("UA", "<@UB> hi"))
	}
	if st, _ := repo.Stats(context.Background(), "UB"); st.PropsiesReceived != 8 {
		t.Errorf("received = %d, want 8", st.PropsiesReceived)
	}
}

func TestUsageShowsAllowance(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	h.Settings = Settings{Limit: 200, LimitPeriod: 30 * 24 * time.Hour, PerPersonLimit: 5, PerPersonLimitPeriod: 24 * time.Hour}
	_ = h.HandleSlashCommand(context.Background(), propsy("UA", ""))
	got := fs.lastResponse(t).Text
	for _, want := range []string{usageNeedle, "*200* propsies/dropsies available", "resets 30d after you first use one", "up to 5 per 1d"} {
		if !strings.Contains(got, want) {
			t.Errorf("usage missing %q: %s", want, got)
		}
	}
}

func TestHomeShowsAllowanceAndWhenItResets(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	h.Settings = Settings{Limit: 10, LimitPeriod: 7 * 24 * time.Hour}
	ctx := context.Background()
	t0 := h.Now()
	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> hi"))
	if err := h.PublishHome(ctx, "UA"); err != nil {
		t.Fatal(err)
	}
	out := render(t, fs.published["UA"].Blocks.BlockSet)
	want := "*9* propsies/dropsies available, resetting to 10 <!date^" + strconv.FormatInt(t0.Add(7*24*time.Hour).Unix(), 10)
	if !strings.Contains(out, want) {
		t.Errorf("home missing %q: %s", want, out)
	}
}

func TestAnnounceChannel(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	h.AnnounceChannel = kudosChannel
	ctx := context.Background()

	_ = h.HandleSlashCommand(ctx, propsy("UA", "<@UB> for the help"))
	if len(fs.posts) != 1 || fs.posts[0].channel != kudosChannel || !strings.Contains(fs.posts[0].text, "propsy!") {
		t.Fatalf("posts = %+v, want one announcement in CKUDOS", fs.posts)
	}
	if len(fs.responses) != 1 || fs.responses[0].ResponseType != slack.ResponseTypeEphemeral || !strings.Contains(fs.responses[0].Text, "<#CKUDOS>") {
		t.Errorf("giver should get a private confirmation, got %+v", fs.responses)
	}
}

func TestAnnounceChannelSameAsCurrentChannelPostsInline(t *testing.T) {
	h, fs, _ := newTestHandler(t)
	h.AnnounceChannel = "C1" // the channel propsy() runs in
	_ = h.HandleSlashCommand(context.Background(), propsy("UA", "<@UB> hi"))
	if len(fs.posts) != 0 || fs.lastResponse(t).ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("posts=%+v responses=%+v; want a plain in_channel reply", fs.posts, fs.responses)
	}
}

func TestAnnounceChannelFailureFallsBack(t *testing.T) {
	h, fs, repo := newTestHandler(t)
	h.AnnounceChannel = kudosChannel
	fs.postErr = errors.New("not_in_channel")
	if err := h.HandleSlashCommand(context.Background(), propsy("UA", "<@UB> hi")); err != nil {
		t.Fatalf("err = %v, want nil (fallback)", err)
	}
	if r := fs.lastResponse(t); r.ResponseType != slack.ResponseTypeInChannel {
		t.Errorf("want fallback in_channel announcement, got %+v", r)
	}
	if st, _ := repo.Stats(context.Background(), "UB"); st.PropsiesReceived != 1 {
		t.Errorf("the propsy should still be recorded")
	}
}
