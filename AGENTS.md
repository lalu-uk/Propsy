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
  SQLite itself is due to be replaced by a JSON-file store (`PLAN.md` §3, "Local storage").
- Firestore stores counters + a per-recipient subcollection + one board document so every query uses automatic
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
- Two leaderboards, both listing *everyone* they cover (by default; `PROPSY_LEADERBOARD_MAX_USERS` sets a top-N,
  and the hard maximum `config.MaxLeaderboardUsers` = 1000 always guards Slack's block limit:
  config clamps larger values and returns a `Config.Warnings` entry that `main` logs): received (score = propsies − dropsies, scores ≤ 0 included) and given (propsies
  + dropsies both count). In Firestore both boards live in one document, `boards/all`, updated
  with atomic nested-map increments in `AddActions` (1 read per board fetch regardless of headcount;
  the 1 MiB document limit means ~15k users, an accepted ceiling).
- Home tabs are only built when opened (`app_home_opened`), never refreshed after a command: that
  would put up to 11 Slack calls on the 3 s ack path.
- `PROPSY_ANNOUNCE_CHANNEL` posts announcements with `chat:write`; any failure falls back to the
  `response_url` reply in the current channel.
- Webhook handling is synchronous (response = ack, 3 s limit).
- The slash commands rely on `should_escape: true` to get `<@U…>` mentions.

## Committing (agents)

This assumes the owner has a git alias `agent-commit` configured (see CONTRIBUTING.md, "AI-assisted
commits"). Commit with it, not plain `git commit`, setting the model on every commit:

```sh
AGENT_MODEL="<provider>/<model>" git agent-commit -m "type(scope): subject"
```

`AGENT_MODEL` is the model you're actually running as (provider/model ID, as in your system
prompt's model info). The alias takes the same arguments as `git commit`, signs with a dedicated
agent key, and adds an `Assisted-by: OpenCode <version> (<model>)` trailer. If the alias is missing,
don't fall back to plain `git commit` (signing may need the owner to touch a hardware key); ask.

If a `gh` remote exists, fetch and push through it rather than `origin`. Only commit or push when
asked, and open a PR instead of pushing to `main`.

## Releases

Built for GitHub **immutable releases** (enforced at the org level): release-please makes a *draft*
release with its tag already pushed (`release-please-config.json`: `draft` + `force-tag-creation`),
GoReleaser uploads every asset to that draft (`release.use_existing_draft`) and then publishes it
(`draft: false`); after that the release can't change. Never add a step that modifies a release
after the GoReleaser job, and never create the release published. If the job fails before the
publish, re-run the failed job.

The README's install instructions carry the current release version (mise pin, binary URL, image
tag, verification examples). release-please bumps them in each release PR: `release-please-config.json`
lists README.md under `extra-files`, and the lines to bump sit between
`<!-- x-release-please-start-version -->` and `<!-- x-release-please-end -->` (or on a line marked
`x-release-please-version`). Don't edit those versions by hand, and keep other version-like numbers
(Go versions and so on) out of the marked blocks: every semver-looking string in them is replaced.

Everything GoReleaser builds is signed keylessly with cosign (`signs`, `docker_signs`, plus Syft
SBOMs and `actions/attest` provenance; see `PLAN.md` §5 and the README's "Verifying a release").
Signing needs the CI OIDC token, so locally use `goreleaser release --snapshot --clean
--skip=sign,sbom` (add `docker` without buildx). Don't pin a tag in the verification identity: the
workflow runs on `workflow_run`. Container tags are exact versions only: never add `latest`.

## Env vars

Local: `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`, optional `DB_PATH` (default `./propsy.db`).
GCP: `ENVIRONMENT=gcp`, `SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`, `GCP_PROJECT_ID`,
optional `PORT`, `FIRESTORE_DATABASE_ID`.
Both: optional `PROPSY_LIMIT`, `PROPSY_LIMIT_PERIOD`, `PROPSY_PER_PERSON_LIMIT`,
`PROPSY_PER_PERSON_LIMIT_PERIOD`, `PROPSY_LEADERBOARD_MAX_USERS`, `PROPSY_ANNOUNCE_CHANNEL` (see `.env.example`).
