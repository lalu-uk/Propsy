# PLAN.md: Propsy — self-hosted serverless kudos for Slack

## 1. What it is

A single static Go binary: a Slack bot for giving kudos, called **propsies** (singular:
*propsy*), and, opt-in, taking them away with **dropsies** (singular: *dropsy*). The
vocabulary is fixed: "1 propsy", "5 propsies", "1 dropsy", "5 dropsies", everywhere.

- `/propsy @alice @bob for unblocking the release` → public announcement; each recipient
  gets a propsy.
- **App Home** dashboard: your totals (`Received: 5 propsies, 10 dropsies` /
  `Given: 3 propsies, 5 dropsies`; dropsies are hidden while zero), your remaining allowance,
  two all-time **leaderboards** that list everyone (no top-N): *received* (everyone who has
  received a propsy or dropsy, by score = propsies − dropsies, negatives included) and
  *given* (everyone who has given a propsy or dropsy, by how many in total), and what you've recently
  received.

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
- Event `app_home_opened` → `views.publish` for the opener. Home tabs are built only when
  opened, never pushed after a command: a live refresh of the giver and every recipient meant
  up to 11 sequential `views.publish` calls on the 3 s ack path, and a tab is rebuilt on
  reopen anyway. The only staleness is a tab someone is already staring at.
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
rate limits; the received board ranks by net score and includes anyone who has received one, even at
zero or below; Recent shows
dropsies with a :droplet:. `slack/manifest_test.go`
guards that each dropsy manifest differs from its base only by the `/dropsy` command.

## 3. Storage: Firestore (decision)

Firestore over Spanner/Cloud SQL: Spanner has a ~$65/mo/node floor (granular instances
~$0.09/hr) and Cloud SQL bills for an always-on VM; Firestore is pay-per-operation with a
free tier (50k reads / 20k writes per day, 1 GiB), so an idle Propsy costs ≈ $0.

Reviewed again in Oct 2026 against the goal of being serverless and free:

- **Budget.** A give costs `1 + N` reads and `3N + 2` writes for N recipients (so 5 writes
  for one recipient, 32 for ten), and a Home tab costs ~8 reads. The daily free tier covers
  ~4,000 single-recipient gives or ~6,000 Home opens. Only the `(default)` database gets
  the free quota; config warns if `FIRESTORE_DATABASE_ID` names another one.
- **Datastore mode** has the same pricing and free tier with an older API, so there's no
  reason to switch.
- **One Cloud Storage object** (a JSON or SQLite blob written with `ifGenerationMatch`) is
  safe across instances, but the always-free tier is 5,000 Class A ops a month (~160
  writes a day) and only in a few US regions. Every write also rewrites the whole object,
  and contended gives would retry. It's a smaller free tier for more code.
- **SQLite on Cloud Run** (local disk, a GCS FUSE mount, or Litestream) is unsafe with more
  than one instance. Even pinned to one instance, it adds a restore on every cold start
  inside Slack's 3 s ack window. Not worth it.
- **A Go key-value file** (JSON, bbolt) has the same single-writer problem on Cloud Run.
  `local` mode is different: see "Local storage" below.
- **Cloud SQL, AlloyDB, Spanner, Bigtable, Memorystore** all bill while idle (Spanner's free
  instance is a 90-day trial), so they're ruled out.

Verdict: keep Firestore for `gcp` mode.

### Local storage: replace SQLite with a JSON file (decision, planned)

Firestore is the production store, so SQLite is a second data model to keep in step with
it. `local` mode runs as one process, so it doesn't need what SQLite is for (transactions
shared between processes), and its SQL buys nothing the Firestore store doesn't already do
with reads and a sort in Go. The driver also adds ~5 MB to the binary (34 MB vs 29 MB) and
30 `modernc.org` packages.

Plan: a `JSONFile` store that keeps everything in memory and mirrors the Firestore layout
(user records with counters and the overall window, per-pair windows, a received list per
user, one boards record). `AddActions` updates memory under a mutex, then saves by writing a
temp file, syncing it and renaming it over the old one, so a crash never leaves a partial
file. Rewriting the whole file per give is fine at this app's scale (~1.5 MB at 10,000
propsies). The leaderboard sort is shared with Firestore. It must pass the existing
contract test in `store_test.go`.

Migration: on startup, if `DB_PATH` points at an existing SQLite file, import it once into
the JSON file. Keep that importer (and the SQLite driver) for one release, then remove both.
Handler and webhook tests move from `:memory:` SQLite to an in-memory `JSONFile`.

Rejected: bbolt (closest to Firestore's collections, but still a dependency and more code
than the JSON file).

Layout:

- `users/{id}` → counters `propsiesReceived`, `dropsiesReceived`, `propsiesGiven`,
  `dropsiesGiven` (`Increment`), plus the giver's overall rate-limit window (`windowStart`,
  `windowUsed`).
- `boards/all` → both leaderboards in **one document**: `received` (user ID → score) and
  `given` (user ID → count), bumped with atomic nested-map increments inside `AddActions`.
- `users/{id}/received/{auto}` → `{from, to, reason, channelId, delta, createdAt}`.
- `users/{from}/pairs/{to}` → the per-person window (`windowStart`, `windowUsed`).

`AddActions` is one transaction: read the windows, check them (shared pure logic in
`store/limits.go`, so SQLite and Firestore can't disagree), then write the `received`
docs, counters and windows. A limit hit returns a `*LimitError` with nothing written.
Limits cost O(1) reads, with no per-action history to count.

Leaderboards are a single read of `boards/all` and a sort in Go, so a Home tab costs ~8
Firestore reads (boards 1, stats 1, allowance 1, recent ≤5) whatever the headcount. The
alternative, ordering `users` by a counter, is one billed read per row, so it scales with
people × Home opens. The price is Firestore's 1 MiB document limit: about 15,000 users, after
which `AddActions` would start failing. That's an explicit, accepted limit (a workspace that big
can shard the boards). By default the boards cover everyone; `PROPSY_LEADERBOARD_MAX_USERS` (0 = everyone)
sets a top-N instead. Either way there's a hard maximum, `config.MaxLeaderboardUsers` (1000 rows per board),
to stay inside Slack's Home-tab block limit: config lowers a larger setting to 1000 and logs a
warning at startup (a soft clamp rather than a startup failure, since the intent is still
"a lot"), and the handler clamps again as a backstop. The handler reads N+1 rows so a board that was
cut says "Showing the top N." while one that merely has N people doesn't. A top-N saves
nothing on Firestore reads (a board is one document read whatever its size); it only
shortens the tab. Long boards are split across sections of at most 2800
characters (Slack's limit is 3000). Recent = `received` subcollection `ORDER BY createdAt
DESC`. Ties are broken by user ID so the order is stable between renders, and equal counts
share a rank. No composite index is needed anywhere.

## 4. Cloud Run

- Scale to zero (`min-instances=0`); concurrency default. Requests are handled
  **synchronously** (response is the ack; Slack's limit is 3 s). Distroless static image
  → fast cold starts. Escape hatches if cold starts miss the 3 s: `min-instances=1`, then
  CPU-always-allocated + goroutine, then Cloud Tasks.
- Secrets (`SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`) from Secret Manager; runtime service
  account gets `roles/datastore.user` only. `GCP_PROJECT_ID` set as env.
- Image source: Cloud Run deploys the public image straight from ghcr
  (`gcloud run deploy --image=ghcr.io/lalu-uk/propsy:vX.Y.Z`); no extra registry needed. Deploy
  an exact version, since there's no `latest`.
- Image tags: only the exact version is pushed (no `latest`, no floating `1`/`1.2`). ghcr has
  **no tag-immutability setting** (a feature request is open and unanswered), so that's by
  convention, backed by immutable git tags/releases (a version can't be legitimately
  re-released) and by the cosign signature below. Cloud Run pins a revision to the image
  digest at deploy time regardless.

## 5. Build, test, release (copied from ChipIn)

- `mise.toml` pins Go, golangci-lint, GoReleaser, gcloud + Firestore emulator, Java.
- **Tests:** `go test ./...`. SQLite on `:memory:`; Firestore against the emulator
  (`source ./firestore.sh start`), auto-skipped when `FIRESTORE_EMULATOR_HOST` is unset;
  one shared contract test (`testRepository`) runs against both stores. Handler tests use
  fakes; webhook tests drive real handler over `httptest` with hand-computed Slack
  signatures. No live Slack/GCP calls.
- **CI:** golangci-lint v2 + tests with emulator, on PRs and `main`.
- **Release (built for GitHub immutable releases, which this org enforces):** a published
  release's assets and tag are frozen, so everything must be attached before it's published.
  1. Merging release-please's PR makes it push the tag and create a **draft** release with its
     notes (`release-please-config.json`: `"draft": true` plus `"force-tag-creation": true`;
     without the latter GitHub withholds a draft's tag until publish, release-please can't see
     the previous release, and it proposes a bogus next-major PR).
  2. GoReleaser builds everything, signs it, and uploads it all to that draft
     (`release.use_existing_draft`), then publishes it (`draft: false`). Only then does the
     release become immutable.
  3. A failure before the publish leaves an unsealed draft: fix it and re-run the failed job
     (the release-please outputs are reused, so it gets the same tag; `replace_existing_artifacts`
     covers a partial upload). After the publish it's final: a bad release means a new version,
     since a tag can't be reused even if the release is deleted.
  `release.preflight.fail_on_error` makes GoReleaser stop early if the tag is already
  published as an immutable release. Gated on CI passing. Actions pinned by SHA; Renovate keeps
  them fresh.
- **Supply chain: keyless signing, no keys or secrets.** Everything GoReleaser produces is
  signed with cosign using Sigstore keyless signing: the release job's GitHub OIDC token is
  exchanged for a short-lived certificate naming this repo's `release.yml`, and the signature
  is logged in the public Rekor transparency log.
  - Binaries, `checksums.txt` and SBOMs: `signs` (`artifacts: all`), one `.sigstore.json`
    bundle each, attached to the release.
  - The container image: `docker_signs`, signing the manifest by digest.
  - SBOMs: Syft, one per binary (`sboms`); buildx also attaches an SBOM and provenance
    attestation to the image by default.
  - Build provenance: `actions/attest` over `checksums.txt` and `digests.txt`, verifiable with
    `gh attestation verify`. It complements the signature: a signature says who published an
    artifact, provenance says which workflow run built it from which commit.
  - The job runs with only the permissions it needs (`id-token`, `attestations`, `packages`,
    `contents`); the workflow default is none.
  - **Verification identity.** The release workflow is triggered by `workflow_run`, so the
    certificate's workflow ref is the default branch (`…/release.yml@refs/heads/main`), not
    the tag. Verifiers should match the repo + workflow path with a regexp and not pin a ref
    (see README).
  - Signing needs a GitHub OIDC token, so it only works in CI; local snapshot builds use
    `--skip=sign,sbom` (and `docker` if you have no buildx).
  - Signing happens inside the GoReleaser run, before the draft is published, so the signature
    bundles are part of the sealed release. The provenance attestations are made afterwards;
    they live in GitHub's attestation store, not as release assets, so immutability doesn't
    affect them.

## 6. Roadmap

1. **MVP, socket mode (done):** store, handler, home tab, socket + webhook transports,
   CI/release scaffolding, manifests. Verify live with a real Slack app locally.
2. **Release pipeline:** push to a GitHub repo, confirm CI green, merge the first release PR,
   and check the draft is created with its tag, gets all assets (binaries, checksums, SBOMs,
   `.sigstore.json` bundles), is published and shows as immutable, and the signed image lands
   in ghcr. (Each published version is permanent, so expect to burn one or two while shaking
   this out.)
3. **Cloud Run end-to-end:** create GCP project + Firestore (native mode), Secret Manager,
   service account, `gcloud run deploy`; point the
   HTTP manifest at the service; verify `/propsy` and Home over HTTP. Capture the exact
   commands (or Terraform) in `deploy/` + README, and consider a release-triggered deploy
   job using Workload Identity Federation.
4. **Local store → JSON file:** replace SQLite with the `JSONFile` store described in §3
   ("Local storage"), with a one-release SQLite importer.
5. **Later, if wanted:** time-windowed leaderboards, a message
   shortcut for giving a propsy, `PROPSY_REQUIRE_REASON`, `LOG_FORMAT=json` for Cloud Logging.
