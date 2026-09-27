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

## Round 2 — 2026-09-27

### Taken

| PR | Change | Notes |
|---|---|---|
| #367 | qBit `hashes=all` means unfiltered | Resolved in the qBit handlers, not the shared filter. Upstream's version made the web-UI bulk delete `hashes=all` wipe every entry. |
| #418 | qBit setCategory respects `hashes` | Before, an arr post-import category rewrote every entry. |
| #393 | Radarr manual import sends `movieId` | |
| #363 (reworked) | `POST /api/config` is an RFC 7396 merge patch | Lists replace, objects merge, `null` resets. The settings page round-trips hidden fields (`api_host`, RD `limit`). Fixes the rest of upstream #343. |
| #421 | Config-save restart can't leave the server unreachable | Bounded drain; the page waits for a new instance marker. |
| #424 (adapted) | A folder stays served while another entry renders it | In-memory name index: Delete at 10k entries 12 µs instead of the PR's 7 ms. Fixes #423. |
| #312 (partial) | DFS teardown race: phantom stream registrations | |
| #327 (partial) | Names over 255 bytes are truncated with a `~hash` suffix | The ASCII-rename option was dropped. The DFS mount keeps full names. |
| #410 (partial) | Completed queued grabs get their file list | Affected every provider. |
| #394 (adapted) | Zero-file completion retries ~46 s, then fails instead of completing empty | |
| #294 (reworked) | Debrid provider `priority` | Before, provider order was random per restart (xsync map seed). Unset = config position. An uncached TorBox miss falls through by priority. |
| #275 part (a) | `keep_in_sync`: adopt finished provider torrents (e.g. added via DMM) | Opt-in per debrid. No extra provider calls. A hash shared by RD and TorBox is adopted once. |
| #406 (fixed) | AllDebrid: ready magnet with no files isn't done | |
| #426 | Premiumize re-mints the CDN link | Fixes #425. |
| #399 (trimmed) | AllDebrid slot strategies | Opt-in. Supersedes #199. |

### Skipped

| PR | Reason |
|---|---|
| #283 | Beta rewrote the DFS read path. Its classifier made warm reads 21–71% slower (benchstat). |
| #378 | Obsolete: hybrid store removed; appendstore v0.6.0 compacts safely. |
| #325 | Already on beta, except ~62 s of validation retries that would block DFS reads. |
| #174, #262 | Obsolete on beta. |
| #264 | Rewrites names and DFS layout for every `.torrent` upload; conflicts in 5 files. |
| #275 part (b), #321 | Dashboard relabel/moves are out of scope; #321 duplicates #275. |
| #350, #389, #392 | Large bundles; verdicts in `research/bundles.md`. |
| #191 | Carried into round 3 (URL-base-aware redirects). |

Upstream issues already fixed on beta (in addition to round 1): #298, #315, #377, #412.

### Verify on the live setup

- Save settings once. `config.json` keeps a hand-set `limit`/`api_host`, and per-arr `download_uncached` can be reset.
- Behind Pangolin, a restart-requiring save reloads cleanly with no bad gateway.
- The arrs' post-import category changes only that torrent.
- Set `priority` on RD and TorBox; grabs go to the lowest number first, and an uncached TorBox miss falls through to the next provider.
- With `keep_in_sync` on, DMM-added torrents appear under category `other`. A restart adopts nothing new.
- The DFS stats page shows 0 streams after playback stops. Folders shared by two entries survive deleting one of them.

### Test status

Full suite on `dev` 2c86465 with `GOTOOLCHAIN=go1.26.5`: 38 packages pass. Two test fakes needed fixing after the branches met (a0090b1).

### Build note

Rebuild `pkg/server/assets/build/js` with `npm ci --ignore-scripts && node scripts/minify-js.js`. The pinned terser reproduces the committed files byte for byte.

## Round 3 — audit fixes, 2026-09-27

Fixes for the findings in `research/audit.md`. Every fix has a test that fails without it.

| ID | Change | Deployment impact |
|---|---|---|
| H1 | `/browse` XSS fixed. UI HTML sinks are escaped, all inline handlers are gone, and a per-request nonce CSP is set. `GET /api/config` masks secrets as `********`; a save keeps them unless they are replaced or cleared, and it refuses to keep a secret whose destination changed. | Re-enter a secret after changing its provider/host/proxy. |
| H2 | WebDAV and `/stream` require auth whenever `use_auth` is on (UI login, or the API token as Basic password/Bearer). No wildcard CORS. `DELETE` returns 403 unless `webdav_allow_delete`. `enable_webdav_auth` is removed. | WebDAV clients (rclone, players) need credentials when `use_auth` is on. |
| H3 | TLS verification is on for every outbound client. Per-provider opt-out: `insecure_skip_verify`. | Arr/rclone endpoints with a private CA need that CA trusted (`SSL_CERT_FILE`). |
| H4 | Hearsay joins the public P2P network only on opt-in (`hearsay.participate=true`). `-tags nohearsay` builds a stub. | It stops participating unless you opt in. |
| M1 | Tokens and API keys are redacted from URLs in errors and logs. | |
| M2 | Retry/permanent classification uses error types, not message text. | Fewer false 2-minute circuit trips on DFS. |
| M3 | FUSE returns EINTR/ETIMEDOUT/EIO instead of short successful reads. | Players see errors instead of an early EOF. |
| M4 | Shared link fetches outlive their first caller (2-minute cap); each caller honours its own ctx. | |
| M5 | DFS metadata writers park when idle. | |
| M6/M7 | Evaluated, not changed: `research/dfs-memory-disk.md`. | |
| M8 | HTTP timeouts (header 10 s, idle 2 min, none on streams). A bind failure exits with status 1. | |
| M9 | The first stats snapshot is taken asynchronously; startup no longer waits for providers. | |
| M10 | Go 1.26.6 (stdlib CVEs); sonic v1.15.4. The drain tests are version-agnostic. | Rebuild the image. |
| M11 | One rate budget per provider: RD ≤240/min process-wide, TorBox ≤288/min per key, others 250/min. Retries take permits. | `rate_limit` is now a hard cap. |
| L1 | Provider profile caches are synchronised (singleflight, 1 h TTL). | |
| L4 | WebDAV COPY/MOVE return 405. | |
| L5 | Compact JSON responses. | |
| L6 | pprof binds to 127.0.0.1:6060 by default. | |
| L7 | The session cookie is `Secure` behind HTTPS or `X-Forwarded-Proto: https`. | |
| L8 | IPv6 bind addresses work. | |
| #191 | URL-base-aware redirects (upstream PR, adapted). | |
| qBit | `hashes` split on `|`; addTags/removeTags without hashes → 400. | |

Deferred: L10/L11 (refresh invalidation and cancellation); plans are in the DfsFix report and `research/audit.md`.

### Test status

On `dev` after round 3: `go vet` is clean. `go test ./...` passes on Go 1.26.6 (43 packages) and 1.27.1. `-race` is clean on manager, mount, server, debrid and internal. `-tags nohearsay` builds.

### Toolchain

Hold at Go 1.26.x. Move to go1.27.2+ once it ships (golang/go#81404, a net/http Read/Close deadlock in 1.27.1, affects streaming).
