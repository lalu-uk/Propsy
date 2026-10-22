# PLAN.md: Propsy — self-hosted serverless kudos for Slack

## 1. What it is

A single static Go binary: a Slack bot for giving kudos, called **propsies** (singular:
*propsy*), and, opt-in, taking them away with **dropsies** (singular: *dropsy*). The
vocabulary is fixed: "1 propsy", "5 propsies", "1 dropsy", "5 dropsies", everywhere.

- `/propsy @alice @bob for unblocking the release` → public announcement; each recipient
  gets a propsy.
- **App Home** dashboard: your totals (`Received: 5 propsies, 10 dropsies` /
  `Given: 3 propsies, 5 dropsies`; dropsies are hidden while zero), your remaining allowance,
  an all-time **leaderboard** (top 10 by score = propsies − dropsies received, negative
  scores included), and what you've recently received.

Deliberately small. Non-goals for the MVP: multi-workspace/OAuth install, time-windowed
leaderboards, channels/values/tags, rewards, an admin UI.

## 2. Architecture (same shape as ChipIn)

Both modes ship in one binary, selected by `ENVIRONMENT` (`local` | `gcp`). `cmd/propsy`
branches once; everything below is shared.

| Mode | `ENVIRONMENT` | Transport | Storage |
|---|---|---|---|
| **A: Socket** | `local` (default) | Slack Socket Mode (outbound WebSocket) | SQLite file |
| **B: Webhook** | `gcp` | HTTP, signature-verified (`/slack/commands`, `/slack/events`, `/healthz`) | Firestore |

Packages: `internal/config` (env), `internal/store` (`Repository` + SQLite + Firestore),
`internal/handler` (transport-agnostic logic + Block Kit), `internal/socket`,
`internal/webhook`. Handler depends on a narrow `SlackAPI` interface so it's testable with
fakes.

Single-workspace by design (bot token from env) — self-hosted per workspace.

### Slack surface

- Slash commands `/propsy` and `/dropsy` (and nothing else: any other name is refused),
  with **"Escape channels, users, and links"** on, so mentions
  arrive as `<@U123|name>` and can be parsed reliably.
- Event `app_home_opened` → `views.publish` for the opener. Dashboards for the giver and
  recipients are also refreshed (best-effort) after each command.
- Replies use the command's `response_url` (`in_channel` announcement, `ephemeral` errors).
  Unlike `chat.postMessage`, it works in channels the bot hasn't been invited to.
- **Announce channel** (`PROPSY_ANNOUNCE_CHANNEL`, optional): announcements go there via
  `chat.postMessage` instead, and the giver gets an ephemeral "Posted in #channel". Slack
  accepts a channel ID (recommended; the only thing that works for private channels, and
  survives renames) or a `#name` for public channels, so we pass the value straight
  through with no lookup (a name→ID lookup would need `channels:read`). The bot must be in
  the channel (or hold `chat:write.public`, which we don't request). If posting fails, or
  the announce channel is the channel the command ran in, we use the `response_url` path,
  so a misconfiguration degrades to the default behaviour rather than losing the message.
- Scopes: bot `commands` and `chat:write` (the latter only for the announce channel)
  (+ app-level `connections:write` in socket mode).
  Manifests: `slack/manifest.socket.yaml`, `slack/manifest.http.yaml`.

### Rules

- Can't give yourself a propsy or dropsy (self-mentions in a multi-person command are skipped).
- ≤10 recipients per command; reason ≤500 chars; duplicate mentions collapsed.
- **Overall limit:** `PROPSY_LIMIT` (default 200, 0 = off) per `PROPSY_LIMIT_PERIOD`
  (default `30d`; human-friendly `s/m/h/d/w`).
- **Per-person limit:** `PROPSY_PER_PERSON_LIMIT` (default 5, 0 = off) per
  `PROPSY_PER_PERSON_LIMIT_PERIOD` (default `1d`): the most one person can give any single
  other person. A command naming a capped person is rejected whole, naming who's capped.
- **Fixed windows that reset in full.** A giver's window opens when they first give one,
  and once the period has elapsed the allowance is back to the full limit (the next give
  opens a fresh window). Not rolling: nothing trickles back before then. Per-person windows
  work the same way per (giver, recipient). The reset time is shown wherever the allowance
  is. Chosen over global calendar-style boundaries because a 30d period has no natural
  calendar edge, and "resets 30 days after you first use it" is easy to explain.
- Both limits count propsies and dropsies together, and a command that would exceed a
  limit is rejected whole. Enforcement is atomic: the windows are read and updated in the
  same transaction as the action.
- Limits are meant to be visible: the Home tab and `/propsy` with no arguments both show
  the remaining allowance and when it resets ("You have 190 propsies/dropsies available,
  resetting to 200 Nov 14 at 3:04 PM").

### /dropsy (opt-in)

`/dropsy @user [reason]` records a `Delta: -1` action. It's routed by `cmd.Command`, so it
only exists if the Slack app has the command: the `*.dropsy.yaml` manifests add it, the
default ones don't (no server-side flag; the manifest is the single opt-in). Semantics:
propsies and dropsies are counted separately (received and given); both share the giver's
rate limits; the leaderboard ranks by net score and includes anyone who has received one, even at
zero or below; Recent shows
dropsies with a :droplet:. `slack/manifest_test.go`
guards that each dropsy manifest differs from its base only by the `/dropsy` command.

## 3. Storage: Firestore (decision)

Firestore over Spanner/Cloud SQL: Spanner has a ~$65/mo/node floor (granular instances
~$0.09/hr) and Cloud SQL bills for an always-on VM; Firestore is pay-per-operation with a
free tier (50k reads / 20k writes per day, 1 GiB), so an idle Propsy costs ≈ $0.

Layout:

- `users/{id}` → counters `propsiesReceived`, `dropsiesReceived`, `propsiesGiven`,
  `dropsiesGiven`, and `score` (`Increment`), plus the giver's overall rate-limit window
  (`windowStart`, `windowUsed`).
- `users/{id}/received/{auto}` → `{from, to, reason, channelId, delta, createdAt}`.
- `users/{from}/pairs/{to}` → the per-person window (`windowStart`, `windowUsed`).

`AddActions` is one transaction: read the windows, check them (shared pure logic in
`store/limits.go`, so SQLite and Firestore can't disagree), then write the `received`
docs, counters and windows. A limit hit returns a `*LimitError` with nothing written.
Limits cost O(1) reads, with no per-action history to count. Leaderboard =
`users ORDER BY score DESC LIMIT 10` (an O(10) read, no scan; only users with a `score`,
i.e. who have received something, are listed). Recent = `received` subcollection
`ORDER BY createdAt DESC`. Everything uses only automatic single-field indexes — no
composite index to provision. Ties on the leaderboard are ordered arbitrarily in
Firestore (alphabetical in SQLite); acceptable for MVP.

## 4. Cloud Run

- Scale to zero (`min-instances=0`); concurrency default. Requests are handled
  **synchronously** (response is the ack; Slack's limit is 3 s). Distroless static image
  → fast cold starts. Escape hatches if cold starts miss the 3 s: `min-instances=1`, then
  CPU-always-allocated + goroutine, then Cloud Tasks.
- Secrets (`SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`) from Secret Manager; runtime service
  account gets `roles/datastore.user` only. `GCP_PROJECT_ID` set as env.
- Image source: the release publishes to `ghcr.io/laluowen/propsy`, but Cloud Run can't
  pull from ghcr directly. Use an **Artifact Registry remote repository** (proxy for
  ghcr.io) and deploy `…-docker.pkg.dev/PROJECT/ghcr/laluowen/propsy:<tag>`.

## 5. Build, test, release (copied from ChipIn)

- `mise.toml` pins Go, golangci-lint, GoReleaser, gcloud + Firestore emulator, Java.
- **Tests:** `go test ./...`. SQLite on `:memory:`; Firestore against the emulator
  (`source ./firestore.sh start`), auto-skipped when `FIRESTORE_EMULATOR_HOST` is unset;
  one shared contract test (`testRepository`) runs against both stores. Handler tests use
  fakes; webhook tests drive real handler over `httptest` with hand-computed Slack
  signatures. No live Slack/GCP calls.
- **CI:** golangci-lint v2 + tests with emulator, on PRs and `main`.
- **Release:** release-please (Conventional Commits → release PR → tag) then GoReleaser
  (static binaries ×6 platforms + multi-arch ghcr image), gated on CI passing. Actions
  pinned by SHA; Renovate keeps them fresh.

## 6. Roadmap

1. **MVP, socket mode (done):** store, handler, home tab, socket + webhook transports,
   CI/release scaffolding, manifests. Verify live with a real Slack app locally.
2. **Release pipeline:** push to a GitHub repo, confirm CI green, merge first release PR,
   confirm image lands in ghcr.
3. **Cloud Run end-to-end:** create GCP project + Firestore (native mode), Artifact
   Registry remote repo, Secret Manager, service account, `gcloud run deploy`; point the
   HTTP manifest at the service; verify `/propsy` and Home over HTTP. Capture the exact
   commands (or Terraform) in `deploy/` + README, and consider a release-triggered deploy
   job using Workload Identity Federation.
4. **Later, if wanted:** time-windowed leaderboard, "most given" leaderboard, a message
   shortcut for giving a propsy, `PROPSY_REQUIRE_REASON`, `LOG_FORMAT=json` for Cloud Logging.
