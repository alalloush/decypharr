# Merge log

Upstream PRs brought into `dev`, one commit per PR with the original author kept. The details and adaptations are in each commit body.

## Round 1 — 2026-09-27 (base: upstream `beta` 249ac9e)

### Taken

| PR | Change | Notes |
|---|---|---|
| #360 | Refuse `RemoveAll` at or above the category directory | Refused paths skip file removal but still drop the queue entry. |
| #366 | Tautulli webhook: 400 on targetless payloads (no full sweep) | Auth half already on beta (06c8004). Query-string token forms were not ported, because they leak into logs. |
| #340 | DFS listings skip internal metadata records | Extended to virtual folders and the preview. |
| #348 | Order RAR5 volumes by embedded volume number | Rebuilds beta's segment index. A single unnumbered volume sorts first (RAR5 spec). |
| #432 (+#430) | SAB enqueue failures return 200 `status:false` | #430's `io.ReadAll` handling folded in. |
| #411 | Skip validation for already-expired links | |
| #402 | Provider 400 is refetchable | |
| #369 | Transient or unknown provider codes are not cached as permanent | 500/502/504 are retryable. |
| #370 | TorBox: check the cache before `createtorrent` when `download_uncached=false` | A miss answers qBit 404 `torrent_not_cached`. |
| #324 | TorBox: resolve the CDN URL once and cache it for at most 1h | Reverses beta's `redirect=true` permalinks. Fixes upstream #300 together with #411. |
| #381 | Bounded `requestdl` on persistent CDN 404 | 5-minute refetch cooldown per file; also covers mid-stream `Refresh` (DFS reads). |
| #431 | TorBox usenet downloads listed alongside torrents | Only fetched for Pro accounts; items resolve via `/usenet/requestdl`. Implements upstream #341. |

Fork additions: a configurable debrid `api_host` (a test seam; the UI settings form does not know the field, so a UI save drops it), `fork/spec/config.schema.json` + generator, `fork/spec/env.md` + drift test, and TorBox docs.

### Skipped

| PR | Reason |
|---|---|
| #361 | Obsolete on beta: arr registry keeps host/token after empty-credential polls (verified). |
| #362 | Does not reproduce on beta. `*bool` would force one restart on the first UI save. |
| #347 | Obsolete: the retryablehttp logger was removed on beta (b3f9bf2, c78e333). |
| #305 | Already on beta (ce03e70). |
| #183 | Permalinks already on beta (54742ba); superseded by #324. |
| #328 | Titular fix already on beta (55044d2); the rest is out of scope or already present. |
| #334 | Partly on beta; retry-config contradiction; covered by #402/#369. |
| #368 | Obsolete after the stream session rewrite. |

### Open upstream issues already fixed on beta

#295 (ce03e70), #302 (7f54ad9), #379 (e396a05), #385 (1640aff), #397 (c6177f5). `research/issues.md` still lists these as open.

### Verify on the live setup

- **TorBox + DFS:** expect about one `requestdl` per file per hour. Streams resume after idle or a CDN rejection. Watch for "Link refetch on cooldown" debug lines and API 429s.
- **TorBox Pro usenet:** items appear in `__all__` and stream. Deleting one in decypharr does not remove it on TorBox, so it returns on the next refresh.
- **`download_uncached=false`:** uncached grabs fail fast. TorBox `checkcached` answers from an hour-long cache.
- **Real-Debrid:** a validation 404 now refetches, at most once per 5 minutes per file.

### Test status

Full `go test ./...` on integrated `dev`: 37 packages pass. The only failures are `internal/request` `TestDrainAndCloseEnablesReuse` and `TestDrainGivesUpPastLimit`, and only under local Go 1.27.1; they pass with the `go.mod` toolchain (`GOTOOLCHAIN=go1.26.5`). These must be fixed before a Go 1.27 bump.

### Worker note

`git stash` is shared across all worktrees of this repo. For before/after test checks, copy files aside instead of stashing.
