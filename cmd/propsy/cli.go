package main

import (
	"fmt"
	"io"
)

const usageText = `Propsy is a self-hosted kudos bot for Slack: /propsy gives one, /dropsy takes one away.

Usage:
  propsy              run the bot (configured by environment variables)
  propsy --help       show this help
  propsy --version    show the version

The mode comes from ENVIRONMENT:
  local (default)  Socket Mode, data in a JSON file (DB_PATH). Needs SLACK_BOT_TOKEN, SLACK_APP_TOKEN.
  gcp              HTTP webhooks on PORT, data in Firestore. Needs SLACK_BOT_TOKEN, SLACK_SIGNING_SECRET, GCP_PROJECT_ID.

Optional limits and settings are listed in .env.example and the README's Configuration section.
A .env file in the working directory fills in variables that aren't already set.
`

// cli handles the arguments that don't start the bot. It returns the exit
// status, or -1 when the bot should run instead.
func cli(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return -1
	}
	if len(args) > 1 {
		_, _ = fmt.Fprintf(stderr, "propsy: unexpected argument %q\n\n%s", args[1], usageText)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(stdout, usageText)
		return 0
	case "-v", "--version", "version":
		_, _ = fmt.Fprintf(stdout, "Propsy %s\n", version)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "propsy: unknown argument %q\n\n%s", args[0], usageText)
		return 2
	}
}
