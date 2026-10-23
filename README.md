# Propsy

Self-hosted, scale-to-zero **kudos for Slack**, as a single static Go binary.

- `/propsy @alice @bob for unblocking the release` gives each of them a propsy and announces it.
- The app's **Home** tab shows your propsy and dropsy totals, how many you have left to give, a
  two leaderboards (received, and given), and what you've recently received.

| Mode | `ENVIRONMENT` | Transport | Storage | Suited to |
|---|---|---|---|---|
| **Socket** | `local` (default) | Slack Socket Mode | SQLite | Local dev, a box you control |
| **Webhook** | `gcp` | HTTP | Firestore | Cloud Run, scale-to-zero (~$0 idle) |

## Configuration

| Variable | Default | |
|---|---|---|
| `PROPSY_LIMIT` | `200` | Most propsies + dropsies one person can give per window. `0` = unlimited. |
| `PROPSY_LIMIT_PERIOD` | `30d` | Window length: it starts when someone first gives one, and the allowance resets in full once it has elapsed. Units `s`, `m`, `h`, `d`, `w`, combinable: `30s`, `24h`, `2w`, `1d12h`. |
| `PROPSY_PER_PERSON_LIMIT` | `5` | Most one person can give any single other person per window. `0` = unlimited. |
| `PROPSY_PER_PERSON_LIMIT_PERIOD` | `1d` | Window length for the per-person limit, same rules. |
| `PROPSY_LEADERBOARD_MAX_USERS` | `0` | Most people each leaderboard lists. `0` = everyone. Hard maximum 1000: larger values are lowered to 1000 with a startup warning. |
| `PROPSY_ANNOUNCE_CHANNEL` | _(unset)_ | Announce in this channel instead of where the command was run. Channel ID recommended (`C0123ABCDEF`); `#name` works for public channels. Invite the bot first. |

Allowances reset in full once the period has elapsed, not gradually: your window opens when
you first give a propsy or dropsy, and 30 days later (by default) you're back to 200. The
rest (tokens, DB, GCP) are in [`.env.example`](./.env.example). Everyone can see their
remaining allowance and when it resets in the Home tab, or by running `/propsy` with no
arguments.

## /dropsy (optional)

`/dropsy @someone for being a menace` gives a dropsy, which takes a propsy away. It counts
against the giver's limits like a propsy does, and the Home tab shows both totals
(`Received: 5 propsies, 10 dropsies`). A person's score on the received board is propsies minus
dropsies, and negative scores are listed too (at the bottom). It's opt-in because it isn't a
friendly pattern for every team: use `slack/manifest.socket.dropsy.yaml` /
`slack/manifest.http.dropsy.yaml` instead of the default manifests to enable it. Without
that command in the Slack app, nothing else changes.

## Leaderboards

The Home tab has two boards, and by default they list **everyone**:

- **Received:** everyone who has received a propsy or dropsy, ranked by score (propsies minus
  dropsies received), including zero and negative scores.
- **Given:** everyone who has given a propsy or dropsy, ranked by how many they've given in total
  (propsies and dropsies both count).

Equal counts share a rank. Set `PROPSY_LEADERBOARD_MAX_USERS` to show only the top N of each
board (a board that was cut says "Showing the top N."). There's a hard maximum of 1000 rows per
board (about 13 of the 100 blocks Slack allows on a Home tab), so `0` means "everyone, up to
1000", and a larger setting is lowered to 1000 with a warning in the startup log.

See [`PLAN.md`](./PLAN.md) for design and roadmap, [`AGENTS.md`](./AGENTS.md) for dev notes.

## Run locally (Socket Mode)

1. Create a Slack app from [`slack/manifest.socket.yaml`](./slack/manifest.socket.yaml).
2. Install it to your workspace; copy the bot token. Under *Basic Information → App-Level
   Tokens* create one with `connections:write`.
3. Run, with the tokens in a `.env` file (copy [`.env.example`](./.env.example)) or exported:

   ```sh
   go run ./cmd/propsy
   ```

   A `.env` in the working directory is only a fallback: variables already set in the
   environment take precedence.

Try `/propsy @someone for being great`, then open the app's Home tab.

## Run on Cloud Run (Webhook Mode)

Use a separate Slack app from [`slack/manifest.http.yaml`](./slack/manifest.http.yaml)
(Socket Mode and HTTP URLs are mutually exclusive). Env: `ENVIRONMENT=gcp`,
`SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`, `GCP_PROJECT_ID`. The runtime service account
needs `roles/datastore.user`. Deployment steps are tracked in `PLAN.md` §6.

## Development

```sh
mise install                    # pinned toolchain
source ./firestore.sh start     # optional: enables the Firestore tests
go test ./... && golangci-lint run
```

Releases: Conventional Commits → release-please PR → GoReleaser publishes binaries and
`ghcr.io/laluowen/propsy`.
