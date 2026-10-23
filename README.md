# Propsy

Self-hosted, scale-to-zero **kudos for Slack**, as a single static Go binary.

- `/propsy @alice @bob for unblocking the release` gives each of them a propsy and announces it.
- The app's **Home** tab shows your propsy and dropsy totals, how many you have left to give, two
  leaderboards (received, and given), and what you've recently received.

| Mode | `ENVIRONMENT` | Transport | Storage | Suited to |
|---|---|---|---|---|
| **Socket** | `local` (default) | Slack Socket Mode | SQLite | Local dev, a box you control |
| **Webhook** | `gcp` | HTTP | Firestore | Cloud Run, scale-to-zero (~$0 idle) |

## Install

Latest release: **v1.2.1** <!-- x-release-please-version -->

See the [releases page](https://github.com/lalu-uk/Propsy/releases) for notes.

<!-- Everything between the start and end markers is bumped to each new version by release-please
     (see release-please-config.json). Keep other version-like numbers out of the marked blocks. -->

### mise

[mise](https://mise.jdx.dev) installs the binary straight from the GitHub release:

<!-- x-release-please-start-version -->
```sh
mise use -g github:lalu-uk/Propsy@latest   # the newest release
mise use -g github:lalu-uk/Propsy@1.2.1    # or pin this exact version
```
<!-- x-release-please-end -->

Then run `propsy`. By default mise holds releases back for 24 hours (`minimum_release_age`),
so `@latest` only picks a release up a day after it's published; pin the exact version to get
it straight away.

### Binary

<!-- x-release-please-start-version -->
```sh
# linux/amd64 shown; the other files are propsy_{linux,darwin,windows}_{amd64,arm64}[.exe]
curl -fsSLO https://github.com/lalu-uk/Propsy/releases/download/v1.2.1/propsy_linux_amd64
chmod +x propsy_linux_amd64
```
<!-- x-release-please-end -->

### Container image

<!-- x-release-please-start-version -->
```sh
docker pull ghcr.io/lalu-uk/propsy:v1.2.1
```
<!-- x-release-please-end -->

There is no `latest` tag: always use an exact version. To check what you've downloaded, see
[Verifying a release](#verifying-a-release).

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
needs `roles/datastore.user`. Cloud Run runs the public image straight from ghcr (use an exact
version: there's no `latest`):

<!-- x-release-please-start-version -->
```sh
gcloud run deploy propsy --image=ghcr.io/lalu-uk/propsy:v1.2.1 --region=<region> ...
```
<!-- x-release-please-end -->

Deployment steps are tracked in `PLAN.md` §6.

## Development

```sh
mise install                    # pinned toolchain
source ./firestore.sh start     # optional: enables the Firestore tests
go test ./... && golangci-lint run
```

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the contribution workflow.

Releases: Conventional Commits → release-please PR → GoReleaser publishes binaries and a
versioned image, `ghcr.io/lalu-uk/propsy:vX.Y.Z`. There is deliberately no `latest` tag (or
floating `1` / `1.2`): pin an exact version, or better its digest.

### Verifying a release

Everything is signed with [cosign](https://github.com/sigstore/cosign) using Sigstore
**keyless** signing, so there are no keys to trust or rotate: the signature proves the artifact
was produced by this repository's release workflow. Each binary, `checksums.txt` and SBOM has
a `<name>.sigstore.json` bundle beside it on the release.

<!-- x-release-please-start-version -->
```sh
# A binary (swap in your platform's file name):
cosign verify-blob propsy_linux_amd64 \
  --bundle propsy_linux_amd64.sigstore.json \
  --certificate-identity-regexp '(?i)^https://github\.com/lalu-uk/propsy/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# The container image (best: verify, then pin the digest it resolves to):
cosign verify ghcr.io/lalu-uk/propsy:v1.2.1 \
  --certificate-identity-regexp '(?i)^https://github\.com/lalu-uk/propsy/\.github/workflows/release\.yml@' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# Build provenance (which workflow run built it, from which commit):
gh attestation verify propsy_linux_amd64 --repo lalu-uk/propsy
gh attestation verify oci://ghcr.io/lalu-uk/propsy:v1.2.1 --repo lalu-uk/propsy
```
<!-- x-release-please-end -->

The identity is matched by regexp because the release workflow runs on `workflow_run`, so the
certificate names the branch it ran on, not the tag.

Releases are built to work with GitHub's immutable releases: release-please creates a draft
release (with its tag), GoReleaser attaches every asset to it, and only then publishes it, at
which point the assets and tag are frozen. A published version is permanent: a bad release is
fixed by cutting a new version, not by editing the old one.

To try a release build locally, skip the steps that need CI credentials:
`goreleaser release --snapshot --clean --skip=sign,sbom` (add `docker` without buildx).
