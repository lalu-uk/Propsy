// Package socket adapts Slack Socket Mode events onto the transport-agnostic
// handler.Handler. It owns the receive loop, acknowledges requests, and
// routes slash commands and events to the handler.
package socket

import (
	"context"
	"log"

	"github.com/laluowen/Propsy/internal/handler"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Runner consumes Socket Mode events and dispatches them to the handler.
type Runner struct {
	sm      *socketmode.Client
	handler *handler.Handler
	log     *log.Logger
}

// New builds a Runner. api must be constructed with both the bot and app
// tokens (see slack.New with slack.OptionAppLevelToken).
func New(api *slack.Client, h *handler.Handler, logger *log.Logger) *Runner {
	return &Runner{sm: socketmode.New(api), handler: h, log: logger}
}

// Run connects and processes events until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	go r.loop(ctx)
	return r.sm.RunContext(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-r.sm.Events:
			r.dispatch(ctx, evt)
		}
	}
}

func (r *Runner) dispatch(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		r.log.Println("connecting to Slack via socket mode...")
	case socketmode.EventTypeConnected:
		r.log.Println("connected")
	case socketmode.EventTypeInvalidAuth:
		r.log.Println("invalid auth: check SLACK_APP_TOKEN and SLACK_BOT_TOKEN")

	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		r.ack(evt)
		if err := r.handler.HandleSlashCommand(ctx, cmd); err != nil {
			r.log.Printf("slash command error: %v", err)
		}

	case socketmode.EventTypeEventsAPI:
		ev, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		r.ack(evt)
		if home, ok := ev.InnerEvent.Data.(*slackevents.AppHomeOpenedEvent); ok && home.Tab == "home" {
			if err := r.handler.PublishHome(ctx, home.User); err != nil {
				r.log.Printf("publish home error: %v", err)
			}
		}
	}
}

func (r *Runner) ack(evt socketmode.Event) {
	if evt.Request == nil {
		return
	}
	if err := r.sm.Ack(*evt.Request); err != nil {
		r.log.Printf("ack %s: %v", evt.Type, err)
	}
}
