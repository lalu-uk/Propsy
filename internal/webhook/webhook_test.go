package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/laluowen/Propsy/internal/handler"
	"github.com/laluowen/Propsy/internal/store"
	"github.com/slack-go/slack"
)

const (
	testSigningSecret = "test-signing-secret"
	keyUserID         = "user_id"
	keyText           = "text"
)

type fakeSlack struct {
	responses []*slack.WebhookMessage
	published []string
}

func (f *fakeSlack) PublishView(userID string, _ slack.HomeTabViewRequest, _ string) (*slack.ViewResponse, error) {
	f.published = append(f.published, userID)
	return &slack.ViewResponse{}, nil
}

func (f *fakeSlack) PostMessage(channelID string, _ ...slack.MsgOption) (string, string, error) {
	return channelID, "1.1", nil
}

func (f *fakeSlack) Respond(_ context.Context, _ string, m *slack.WebhookMessage) error {
	f.responses = append(f.responses, m)
	return nil
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func newTestServer(t *testing.T) (*Server, *fakeSlack, store.Repository) {
	t.Helper()
	repo, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	fs := &fakeSlack{}
	return New(handler.New(fs, repo), testSigningSecret, log.New(testWriter{t}, "", 0)), fs, repo
}

// sign computes Slack's v0 signature: HMAC-SHA256("v0:"+ts+":"+body, secret).
func sign(secret string, body []byte) http.Header {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "v0:%s:", ts)
	_, _ = mac.Write(body)
	h := http.Header{}
	h.Set("X-Slack-Request-Timestamp", ts)
	h.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return h
}

func post(t *testing.T, s *Server, path, contentType, body, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header = sign(secret, []byte(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func TestCommandGivesProps(t *testing.T) {
	s, fs, repo := newTestServer(t)
	form := url.Values{
		"command": {"/propsy"}, keyUserID: {"UA"}, "channel_id": {"C1"},
		keyText: {"<@UB|bob> for the help"}, "response_url": {"http://resp"},
	}
	rec := post(t, s, "/slack/commands", "application/x-www-form-urlencoded", form.Encode(), testSigningSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if st, _ := repo.Stats(context.Background(), "UB"); st.PropsiesReceived != 1 {
		t.Errorf("propsies received = %d, want 1", st.PropsiesReceived)
	}
	if len(fs.responses) != 1 {
		t.Errorf("responses = %d, want 1", len(fs.responses))
	}
}

func TestDropsyCommandRoutes(t *testing.T) {
	s, _, repo := newTestServer(t)
	form := url.Values{
		"command": {"/dropsy"}, keyUserID: {"UA"}, "channel_id": {"C1"},
		keyText: {"<@UB|bob> menace"}, "response_url": {"http://resp"},
	}
	rec := post(t, s, "/slack/commands", "application/x-www-form-urlencoded", form.Encode(), testSigningSecret)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if st, _ := repo.Stats(context.Background(), "UB"); st.DropsiesReceived != 1 {
		t.Errorf("dropsies received = %d, want 1", st.DropsiesReceived)
	}
}

func TestBadSignatureRejected(t *testing.T) {
	s, _, repo := newTestServer(t)
	form := url.Values{keyUserID: {"UA"}, keyText: {"<@UB> hi"}}
	for _, path := range []string{"/slack/commands", "/slack/events"} {
		rec := post(t, s, path, "application/x-www-form-urlencoded", form.Encode(), "wrong-secret")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", path, rec.Code)
		}
	}
	if board, _ := repo.Leaderboard(context.Background(), 10); len(board) != 0 {
		t.Errorf("unsigned request must not record propsies")
	}
}

func TestURLVerification(t *testing.T) {
	s, _, _ := newTestServer(t)
	rec := post(t, s, "/slack/events", "application/json",
		`{"type":"url_verification","challenge":"abc123","token":"x"}`, testSigningSecret)
	if rec.Code != http.StatusOK || rec.Body.String() != "abc123" {
		t.Errorf("got %d %q", rec.Code, rec.Body)
	}
}

func TestAppHomeOpenedPublishesHome(t *testing.T) {
	s, fs, _ := newTestServer(t)
	body := `{"type":"event_callback","event":{"type":"app_home_opened","user":"UA","tab":"home"}}`
	rec := post(t, s, "/slack/events", "application/json", body, testSigningSecret)
	if rec.Code != http.StatusOK || len(fs.published) != 1 || fs.published[0] != "UA" {
		t.Errorf("status=%d published=%v", rec.Code, fs.published)
	}

	fs.published = nil
	body = `{"type":"event_callback","event":{"type":"app_home_opened","user":"UA","tab":"messages"}}`
	post(t, s, "/slack/events", "application/json", body, testSigningSecret)
	if len(fs.published) != 0 {
		t.Errorf("messages tab should not publish home")
	}
}

func TestHealthz(t *testing.T) {
	s, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}
