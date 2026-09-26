# Decypharr fork: clean up in Go, then port from a spec

Measured 2026-09-27 against upstream `beta` `249ac9e` (clone in `/tmp/decypharr-betacheck`).

## Sequence

1. Fork from `beta` `249ac9e`. Merge the PRs in the order given in `prs.md`, starting with the safety fixes #360/#361/#362/#366. Do TorBox and Real-Debrid work before the other providers.
2. Each cleanup PR also adds to the spec below. That spec is the only thing that makes the later port mechanical.
3. The fork gets a black-box conformance suite. The Go fork must pass it before any port starts.
4. Port once that suite is green. Rust is the assumed target language; confirm it at that point. The port passes the same suite. The Go fork stays as the reference oracle.

## Scale (non-test Go lines)

| Package | Lines | Port difficulty from a spec |
|---|---:|---|
| `pkg/usenet` + `internal/nntp` | 13,728 + 3,907 | Hard: NNTP, yEnc (`rapidyenc`), 7z streaming (`sevenzip`). A spec won't cover it. |
| `pkg/manager` | 8,315 | Medium: orchestration and state machines. Needs behaviour docs. |
| `pkg/mount` (`dfs` 5,825) | 6,693 | Hard: a custom FUSE VFS (`go-fuse`, `cgofuse`, `facetfs`). |
| `pkg/arr` | 6,445 | Medium |
| `pkg/server` (`qbit` 1,331, `sabnzbd` 1,405, `webdav` 690) | 6,367 | Easy once specced: HTTP surfaces |
| `pkg/debrid` (RD 1,271, TorBox 959, AllDebrid 961, Premiumize 882, DebridLink 840) | 6,280 | Easy once specced: API clients |
| `pkg/storage` | 4,483 | Medium: `appendstore` format (the upstream author's own library) |
| `internal/config` | 2,831 | Easy once specced |
| `pkg/repair` | 2,349 | Medium: behaviour docs |
| `pkg/share` (SMB, NFS servers) | 1,044 | Hard: protocol servers |
| **Total** | **70,288** (+22,438 test) | |

UI: 5,880 lines of `html/template` and 6,864 lines of source JS (`pkg/server/templates`, `pkg/server/assets`).

## Port scope from al's install (checked 2026-09-27)

This comes from `/opt/conf/decypharr/config.json` on al-cachy (keys and flags only; secrets not read).

- **Debrid providers:** `realdebrid` and `torbox`.
- **Mount:** `"type": "dfs"` at `/mnt/decypharr`. That is decypharr's own FUSE VFS (`pkg/mount/dfs`, 5,825 lines), not the rclone mount, so the DFS mount is **in** port scope and is the hardest part of it.
- **Usenet:** zero providers configured. It stays out of port scope; the Go fork keeps it.
- **SMB/NFS:** only ports are set (1445/20490), with no `enabled` flag, so they are treated as unused [INFERENCE]. Out of port scope.
- **`arrs`:** the list is empty.
- **Database:** `appendstore` is 2,777 lines of stdlib-only Go. It is an append-only key/value log with checksums and compaction. The port does not need to reproduce its on-disk format: export JSON from the Go fork, import it into SQLite or redb on the port side. That is easy [INFERENCE].
- **TorBox usenet:** the TorBox provider has no usenet code; its only usenet reference is a stats field (`pkg/debrid/providers/torbox/types.go:149`). Decypharr's usenet support talks to NNTP servers directly. The test suite already contains a fake NNTP server with yEnc articles (`internal/testutil/nntpd`).

## Facts that shape the spec

- **HTTP surface:** 109 chi route registrations in `pkg/server`. There is no OpenAPI or Swagger file in the repository.
- **Provider hosts are hard-coded:** `realdebrid.go:84` (`https://api.real-debrid.com/rest/1.0`) and `torbox.go:98` (`https://api.torbox.app/v1`). The fork needs a configurable host as a test seam before a fake provider can be plugged in.
- **Persistence:** `appendstore` files (`queue.db`, `arr_bindings.db`, `reacquire_jobs.db`) plus JSON (`config.json`, `auth.json`, `torrents.json`, …).
- **Headless mode already exists:** `DECYPHARR_*` environment overrides, for example `DECYPHARR_DEBRIDS__0__API_KEY`, and `DECYPHARR_AUTH_TOKEN_ONLY` "for headless deployments that never open the web UI" (`internal/config/env.go`). Compose then only needs the tokens and the volumes.
- **Go version:** `go.mod` already targets 1.26.5. Local Go 1.27.1 logs that `bytedance/sonic` does not support it and falls back to `encoding/json`. A later Go bump needs a sonic update or its removal.

## Spec artifacts (language-neutral source; generate TS/Rust from it)

| Artifact | How to produce it from the Go fork | Consumers |
|---|---|---|
| Config JSON Schema (`config.json` + env mapping) | Generate from `internal/config` structs with `invopop/jsonschema` (MIT). Document each `DECYPHARR_*` key. | TS types (json-schema-to-typescript); Rust structs (`typify`, Apache-2.0) |
| OpenAPI 3.1 for the app API | Build the route inventory from `chi.Walk`. Generate request/response schemas from the Go structs, or migrate handlers to `huma` (MIT, code-first OpenAPI on chi). | UI client, port server stubs |
| Compat surface subsets | qBittorrent WebUI API v2 and SABnzbd API are documented upstream. Record which calls Sonarr/Radarr actually make against the fork and specify only that subset. The same approach applies to WebDAV. | Conformance suite |
| Provider fixtures | Record RD and TorBox request/response pairs with tokens redacted. Replay them through a fake provider server, using the host seam above. | Both implementations' tests |
| Conformance suite | A Bun test suite that starts any implementation with a config and the fake provider, then asserts HTTP/WebDAV behaviour and on-disk results (symlinks, STRM). | Go fork first, then the port |
| Behaviour docs | Mermaid state diagrams for the job/torrent lifecycle, repair and link refresh. Document limits, retries and schedules. Tie each item to the test that pins it. | Porter |
| Data export | A documented JSON export of `appendstore`/JSON state, so the port can import existing installs. | Migration |

A spec plus the suite makes the config, providers, qBit/SAB/WebDAV APIs and UI API mechanical to port. It does not make usenet, the FUSE VFS or SMB/NFS easy. Decide their scope before the port; candidates for later are usenet, SMB/NFS, the non-RD/TorBox providers and the internal DFS mount.

## Runtime weight (idle RSS, measured locally 2026-09-27)

| Process | RSS | Binary |
|---|---:|---:|
| decypharr beta, headless, no providers configured | 35.1 MB (14 threads) | 37.2 MB stripped |
| Go `net/http` hello world | 8.9 MB | 5.9 MB |
| Bun `Bun.serve` hello world | 12.8 MB | — |
| Rust axum hello world | 4.2 MB | 1.9 MB |

Rust has the lowest floor. How decypharr's RSS compares under real load (caches, VFS, streaming) is unmeasured.

## Web UI for a port

- **Rust-only:** askama/maud templates + htmx. This is the same shape as today's Go templates: no JS build step, and one language.
- **Codarr default:** SvelteKit built with adapter-static and embedded in the binary.

In both cases the server serves embedded static bytes, so the server-side weight difference is negligible [INFERENCE]. The real differences are the development workflow and the browser payload. The choice stays open.
