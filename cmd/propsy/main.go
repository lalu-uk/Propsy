// Command Propsy is a self-hosted kudos bot (Propsies and Dropsies) for Slack.
// It runs in one of two modes, selected by the ENVIRONMENT variable (see
// PLAN.md §2 and internal/config):
//
//   - ENVIRONMENT=local (default): Socket Mode transport + a JSON data file.
//     Suited to a single long-running process with outbound-only networking.
//   - ENVIRONMENT=gcp: HTTP webhook transport + Firestore storage. Suited to
//     Cloud Run, including scale-to-zero.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/laluowen/Propsy/internal/config"
	"github.com/laluowen/Propsy/internal/handler"
	"github.com/laluowen/Propsy/internal/socket"
	"github.com/laluowen/Propsy/internal/store"
	"github.com/laluowen/Propsy/internal/webhook"
	"github.com/slack-go/slack"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if code := cli(os.Args[1:], os.Stdout, os.Stderr); code >= 0 {
		os.Exit(code)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Propsy:", err)
		os.Exit(1)
	}
}

func run() error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	logger.Printf("Propsy %s starting", version)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, w := range cfg.Warnings {
		logger.Println("warning:", w)
	}

	switch cfg.Environment {
	case config.EnvGCP:
		return runWebhook(cfg, logger)
	default:
		return runSocket(cfg, logger)
	}
}

// newHandler wires the shared handler; both modes build it identically.
func newHandler(cfg *config.Config, c *slack.Client, repo store.Repository) *handler.Handler {
	h := handler.New(handler.Slack{Client: c}, repo)
	h.Settings = handler.Settings{
		Limit:                cfg.Limit,
		LimitPeriod:          cfg.LimitPeriod,
		PerPersonLimit:       cfg.PerPersonLimit,
		PerPersonLimitPeriod: cfg.PerPersonLimitPeriod,
		AnnounceChannel:      cfg.AnnounceChannel,
		LeaderboardMax:       cfg.LeaderboardMaxUsers,
	}
	return h
}

// runSocket runs Mode A: Socket Mode transport + a JSON data file.
func runSocket(cfg *config.Config, logger *log.Logger) error {
	repo, err := store.NewJSONFile(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()

	slackClient := slack.New(cfg.SlackBotToken, slack.OptionAppLevelToken(cfg.SlackAppToken))

	h := newHandler(cfg, slackClient, repo)
	runner := socket.New(slackClient, h, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx); ctx.Err() == nil {
		return err
	}
	logger.Println("shutting down")
	return nil
}

// runWebhook runs Mode B: HTTP webhook transport + Firestore storage, for
// Cloud Run. Requests are handled synchronously (handler runs to completion
// before the HTTP response is written) — see internal/webhook's doc comment
// for the cold-start/3s-ack tradeoff this implies.
func runWebhook(cfg *config.Config, logger *log.Logger) error {
	ctx := context.Background()
	repo, err := store.NewFirestore(ctx, cfg.GCPProjectID, cfg.FirestoreDatabaseID, cfg.DBPrefix)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()

	slackClient := slack.New(cfg.SlackBotToken)

	h := newHandler(cfg, slackClient, repo)
	srv := webhook.New(h, cfg.SlackSigningSecret, logger)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	runCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on :%s (webhook mode)", cfg.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-runCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	logger.Println("shutting down")
	return nil
}
