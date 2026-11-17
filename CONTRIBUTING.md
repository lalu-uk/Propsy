# Contributing to Propsy

Thanks for helping out. Propsy is small on purpose; see [`PLAN.md`](./PLAN.md) for the design and
what's deliberately out of scope, and [`AGENTS.md`](./AGENTS.md) for the layout and the decisions
behind it.

## Setup

```sh
mise install                    # pinned toolchain (Go, golangci-lint, GoReleaser, Java)
source ./firestore.sh start     # optional: runs the Firestore emulator (needs Java; downloads it once; stops itself after 3h)
go test ./... && golangci-lint run
```

Tests never touch live Slack or GCP. The Firestore tests run against the emulator and skip
themselves when `FIRESTORE_EMULATOR_HOST` is unset; CI always runs them. To try the bot against a
real Slack workspace, see the README (Socket Mode needs no public URL).

## Pull requests

- Keep a change focused, and add or update tests with it. Anything that touches storage should be
  covered in the shared contract tests in `internal/store/store_test.go`, which run against both
  the local JSON file store and Firestore.
- Use [Conventional Commits](https://www.conventionalcommits.org) for commit messages (`feat:`,
  `fix:`, `docs:`, `ci:`, `chore:` ...), because releases are generated from them: `fix:` makes a
  patch release, `feat:` a minor one, and `!` or a `BREAKING CHANGE:` footer a major one. `docs:`,
  `ci:` and `chore:` don't cut a release on their own.
- Don't hand-edit the version numbers in the README's install and verification examples: release
  PRs update them (the marked blocks, see `AGENTS.md`).
- Releases are GitHub *immutable* releases, so a published version can never be edited. The
  pipeline is described in the README and `PLAN.md` §5.
