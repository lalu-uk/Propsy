# Propsy — Agent/Developer Notes

Self-hosted kudos bot ("propsies", and opt-in "dropsies") for Slack. One static Go binary, two modes selected by
`ENVIRONMENT`: `local` (Socket Mode + SQLite) and `gcp` (HTTP webhooks + Firestore, Cloud
Run). See `PLAN.md` for design and roadmap.

## Layout

- `cmd/propsy` — entrypoint; `run()` branches into `runSocket` or `runWebhook`.
- `internal/store` — `Repository` interface; `SQLite` (pure-Go, `modernc.org/sqlite`) and `Firestore`.
- `internal/handler` — transport-agnostic logic (`HandleSlashCommand`, `PublishHome`) + Block Kit (`blocks.go`).
- `internal/socket` / `internal/webhook` — transports; adapt Slack events onto the handler.
- `internal/config` — env loading; `./.env` (see `dotenv.go`) fills in vars that are unset in the real environment.
- `slack/` — app manifests for each mode.

## Testing

- `go test ./...` after every non-trivial change; `golangci-lint run` too.
- `internal/store/store_test.go` holds one contract test run against every `Repository`.
  Any new store method gets added there. Firestore runs only with `FIRESTORE_EMULATOR_HOST`
  (`source ./firestore.sh start`) and skips otherwise — never hit live GCP.
- Handler/webhook tests use a fake `SlackAPI` + `:memory:` SQLite; webhook tests sign
  requests by hand. Don't import `*slack.Client` into logic — go through `SlackAPI`.

## Design decisions

- Pure-Go SQLite, **no CGO** (distroless static image, easy cross-compile). Don't switch drivers.
- Firestore stores counters + a per-recipient subcollection so every query uses automatic
  single-field indexes. Don't add queries needing composite indexes without a provisioning story.
- Replies go via `response_url`, not `chat.postMessage`, so the bot needn't be in the channel.
- **Vocabulary:** "1 propsy", "5 propsies", "1 dropsy", "5 dropsies" in every user-facing string,
  doc and comment (use `propsies(n)` / `dropsies(n)` in `blocks.go` for counted nouns). The SQLite
  table is still named `props` to avoid a rename migration.
- Only `/propsy` and `/dropsy` are answered; any other command name is refused. `/dropsy` is
  `Delta: -1`; it's opt-in purely via the `*.dropsy.yaml` manifests (keep them identical to the
  base ones apart from that command: `slack/manifest_test.go` enforces it). Propsies and dropsies
  are counted separately; leaderboard score = propsies − dropsies received.
- Rate limits (`PROPSY_LIMIT[_PERIOD]`, `PROPSY_PER_PERSON_LIMIT[_PERIOD]`; periods parsed by
  `config.ParseDuration`, which adds `d`/`w`) are **fixed windows that reset in full**: a window
  opens on first use and the allowance is back to the limit once the period has elapsed. They're
  enforced atomically inside `Repository.AddActions` (which returns `*store.LimitError`), with the
  window maths in `store/limits.go` (`plan`), shared by both stores. Propsies and dropsies share
  them. They must stay user-visible, with the reset time (Home tab, `/propsy` usage).
- The leaderboard lists everyone who has received a propsy or dropsy, including scores ≤ 0.
- `PROPSY_ANNOUNCE_CHANNEL` posts announcements with `chat:write`; any failure falls back to the
  `response_url` reply in the current channel.
- Webhook handling is synchronous (response = ack, 3 s limit).
- The slash commands rely on `should_escape: true` to get `<@U…>` mentions.

## Env vars

Local: `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`, optional `DB_PATH` (default `./propsy.db`).
GCP: `ENVIRONMENT=gcp`, `SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`, `GCP_PROJECT_ID`,
optional `PORT`, `FIRESTORE_DATABASE_ID`.
Both: optional `PROPSY_LIMIT`, `PROPSY_LIMIT_PERIOD`, `PROPSY_PER_PERSON_LIMIT`,
`PROPSY_PER_PERSON_LIMIT_PERIOD`, `PROPSY_ANNOUNCE_CHANNEL` (see `.env.example`).
