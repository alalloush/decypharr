# Rules

## Git

- Push only to `origin` (github.com/alalloush/decypharr), branch `dev`. `upstream` (sirrobot01) is fetch-only: never push, comment, open PRs or open issues there. `private` (git.por.re) is a stale copy; do not sync it unless al asks.
- Upstream PR import: `git fetch upstream pull/N/head:upstream-pr-N`, then apply only that PR's own diff (many PRs target `main`, which is older than `beta`). One commit per PR: `--author` = the PR author, committer `git -c user.name=omp -c user.email=omp@por.re`, title `<PR title> (upstream #N)`, body with the PR URL and adaptation notes. Record it in `fork/merge-log.md`.
- Fork-original commits use the same `omp` committer identity and a `fork:` title prefix.
- Parallel workers use worktrees under `/tmp/decypharr-wt/<name>`. Never use `git stash`: it is shared across worktrees. For before/after checks copy files aside or use `go test -overlay`.

## Go

- Toolchain: hold at Go 1.26.x (`go.mod` go 1.26.6) until go1.27.2 ships (golang/go#81404). Local Go is 1.27.1, so run tests with `GOTOOLCHAIN=go1.26.6`.
- Every fix needs a test that fails without it.
- Validation before a push: `go vet ./...`; `GOTOOLCHAIN=go1.26.6 go test ./...`; `-race` on `./pkg/manager/... ./pkg/mount/... ./pkg/server/... ./pkg/debrid/... ./internal/...`; `go build -tags nohearsay ./...`.
- After any config struct change: `go generate ./internal/config` and update `fork/spec/env.md`. `TestSchemaUpToDate` and `TestEnvDocMatchesCode` guard both; never hand-edit `fork/spec/config.schema.json`.

## UI assets

- Install with `npm ci --ignore-scripts`. Rebuild JS with `node scripts/minify-js.js` (the pinned terser reproduces the committed build byte for byte) and CSS with `npm run build-css` after template or stylesheet changes. Commit rebuilt `pkg/server/assets/build/` output with its source change.
- Keep the nonce CSP intact: no inline scripts or inline event handlers.

## Scope and secrets

- No Rust or Bun/TS code in this repo until al approves it.
- Do not edit upstream's `docs/` site; fork documentation lives in `fork/`.
- `/opt/conf/decypharr` holds live API keys and secrets: never read or print secret values. Back it up before trialling a fork build.
- Never commit `config.json`, `auth.json`, `.env`, databases or API keys; redact tokens in logs, tests and fixtures.

## Feedback

- Report unclear, missing or wrong skill guidance with `append_feedback`. When you resolve something a consulted skill should have covered, file `append_feedback` (severity `low`, `RESOLVED:` prefix) with the verified pattern.
