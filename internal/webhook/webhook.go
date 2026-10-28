// Package webhook adapts inbound Slack HTTP requests (slash commands and
// Events API callbacks) onto the transport-agnostic handler.Handler, for
// Cloud Run deployments. It is the HTTP counterpart to internal/socket.
//
// Every request is verified against the Slack signing secret before being
// parsed, then handled synchronously: the response is the ack. Slack requires
// one within 3s; if cold starts bite, set min-instances=1 (see PLAN.md).
package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/laluowen/Propsy/internal/handler"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// Server handles inbound Slack webhook requests.
type Server struct {
	handler       *handler.Handler
	signingSecret string
	log           *log.Logger
}

// New builds a Server. signingSecret is the Slack app's signing secret.
func New(h *handler.Handler, signingSecret string, logger *log.Logger) *Server {
	return &Server{handler: h, signingSecret: signingSecret, log: logger}
}

// Routes returns the HTTP handler to serve.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/commands", s.handleCommand)
	mux.HandleFunc("/slack/events", s.handleEvent)
	mux.HandleFunc("/slack/interactions", s.handleInteraction)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// handleCommand processes `/propsy @someone ...`.
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verified(w, r)
	if !ok {
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	cmd, err := slack.SlashCommandParse(r)
	if err != nil {
		s.log.Printf("parse slash command: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.handler.HandleSlashCommand(r.Context(), cmd); err != nil {
		s.log.Printf("slash command error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleInteraction processes interactivity payloads: the give form's
// submission. Other payloads are acknowledged and ignored.
func (s *Server) handleInteraction(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verified(w, r)
	if !ok {
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	cb, err := slack.InteractionCallbackParse(r)
	if err != nil {
		s.log.Printf("parse interaction: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if cb.Type == slack.InteractionTypeViewSubmission {
		if err := s.handler.HandleViewSubmission(r.Context(), cb); err != nil {
			s.log.Printf("view submission error: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleEvent processes Events API callbacks: the one-off url_verification
// handshake and app_home_opened.
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	body, ok := s.verified(w, r)
	if !ok {
		return
	}
	// Signature is already verified; the legacy verification token is not used.
	ev, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		s.log.Printf("parse event: %v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch ev.Type {
	case slackevents.URLVerification:
		var c slackevents.ChallengeResponse
		if err := json.Unmarshal(body, &c); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(c.Challenge))
		return
	case slackevents.CallbackEvent:
		if home, ok := ev.InnerEvent.Data.(*slackevents.AppHomeOpenedEvent); ok && home.Tab == "home" {
			if err := s.handler.PublishHome(r.Context(), home.User); err != nil {
				s.log.Printf("publish home error: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// verified reads the body and checks Slack's signature headers. On failure it
// writes the error response itself and returns ok=false.
func (s *Server) verified(w http.ResponseWriter, r *http.Request) (body []byte, ok bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return nil, false
	}
	if err := verifySignature(r.Header, s.signingSecret, body); err != nil {
		s.log.Printf("signature verification failed: %v", err)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return nil, false
	}
	return body, true
}

func verifySignature(header http.Header, signingSecret string, body []byte) error {
	verifier, err := slack.NewSecretsVerifier(header, signingSecret)
	if err != nil {
		return err
	}
	if _, err := verifier.Write(body); err != nil {
		return err
	}
	if err := verifier.Ensure(); err != nil {
		return errors.New("signature mismatch")
	}
	return nil
}
