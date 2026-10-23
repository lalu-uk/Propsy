// Package socket adapts Slack Socket Mode events onto the transport-agnostic
// handler.Handler. It owns the receive loop, acknowledges requests, and
// routes slash commands and events to the handler.
package socket

import (
	"context"
	"log"
	"sync"

	"github.com/laluowen/Propsy/internal/handler"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	maxConcurrentHandlers = 4
	maxQueuedEvents       = 32
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
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan socketmode.Event, maxQueuedEvents)
	var workers sync.WaitGroup
	for range maxConcurrentHandlers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for evt := range events {
				r.process(runCtx, evt)
			}
		}()
	}

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		r.loop(runCtx, events)
	}()

	err := r.sm.RunContext(runCtx)
	cancel()
	<-loopDone
	close(events)
	workers.Wait()
	return err
}

func (r *Runner) loop(ctx context.Context, events chan<- socketmode.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-r.sm.Events:
			if !ok {
				return
			}
			r.dispatch(evt, events)
		}
	}
}

func (r *Runner) dispatch(evt socketmode.Event, events chan<- socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		r.log.Println("connecting to Slack via socket mode...")
	case socketmode.EventTypeConnected:
		r.log.Println("connected")
	case socketmode.EventTypeInvalidAuth:
		r.log.Println("invalid auth: check SLACK_APP_TOKEN and SLACK_BOT_TOKEN")

	case socketmode.EventTypeSlashCommand:
		_, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		enqueueEvent(events, evt, func() { r.ack(evt) }, r.log)

	case socketmode.EventTypeEventsAPI:
		ev, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if home, ok := ev.InnerEvent.Data.(*slackevents.AppHomeOpenedEvent); ok && home.Tab == "home" {
			enqueueEvent(events, evt, func() { r.ack(evt) }, r.log)
		} else {
			r.ack(evt)
		}
	}
}

// enqueueEvent acknowledges work only after ensuring it fits in the bounded
// queue. With one producer and workers that only remove events, the capacity
// check guarantees the send won't block. A full queue is left unacknowledged so
// Slack can retry the envelope.
func enqueueEvent(events chan<- socketmode.Event, evt socketmode.Event, ack func(), logger *log.Logger) bool {
	if len(events) == cap(events) {
		logger.Printf("socket event queue full; leaving %s unacknowledged for retry", evt.Type)
		return false
	}
	ack()
	events <- evt
	return true
}

func (r *Runner) process(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok {
			return
		}
		if err := r.handler.HandleSlashCommand(ctx, cmd); err != nil {
			r.log.Printf("slash command error: %v", err)
		}

	case socketmode.EventTypeEventsAPI:
		ev, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
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
