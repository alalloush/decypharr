# decypharr (al's fork)

Fork of [sirrobot01/decypharr](https://github.com/sirrobot01/decypharr) (MIT, Go): a debrid manager for Real-Debrid and TorBox with a qBittorrent-compatible API for Sonarr/Radarr, a DFS FUSE mount and WebDAV. Base is upstream `beta` 249ac9e; work happens on `dev`.

Read `fork/PROJECT_STATE.md` at session start and `fork/NEXT_STEPS.md` before picking work. The canonical detail lives in `fork/` (see `fork/README.md`): `merge-log.md` (every taken/skipped PR, audit fixes, test status, live-verify checklists), `research/` (issues, PRs, audit, bundles, DMM, DFS memory/disk, port plan) and `spec/` (generated config schema and env list).

## Strategy

1. Clean up in Go first: merge upstream PRs and fix issues, Real-Debrid and TorBox first.
2. Build the language-neutral spec during that work: config JSON Schema (done), then OpenAPI from the chi routes, provider fixtures through the `api_host` seam, and a black-box conformance suite.
3. Port later (Rust assumed) only after the Go fork passes the conformance suite. No Rust or Bun/TS code in this repo until al approves it.

## Layout

- `main.go`, `cmd/decypharr`, `cmd/healthcheck`, `cmd/schemagen`, `cmd/test-parser`
- `pkg/debrid` — providers behind `common.Client` (realdebrid, torbox first)
- `pkg/manager` — 7.4k-line hub (83 methods, service locator); split is planned, see `fork/research/audit.md` §6
- `pkg/mount` (DFS, `vfs.Backend` seam), `pkg/server` (chi routes, UI, WebDAV), `pkg/arr`, `pkg/repair`, `pkg/storage`, `pkg/usenet`, `pkg/hearsay`
- `internal/config` — `config.Get()` is global (97 call sites); `go generate ./internal/config` writes `fork/spec/`
- UI: server-rendered Go templates + Tailwind v4 + DaisyUI 5 + vanilla JS under `pkg/server/assets`; npm is used only to build assets
- `docs/` is upstream's Astro docs site. Leave it alone; fork docs go in `fork/`.

## al's install

Real-Debrid + TorBox Pro, DFS mount (`mount.type=dfs`) at `/mnt/decypharr`, no usenet providers, SMB/NFS unused. Docker compose stack `/opt/stacks/decypharr` (still the upstream image `cy01/blackhole:latest`; the fork is not deployed), config in `/opt/conf/decypharr`, behind Pangolin. Torrents are added to RD/TorBox through DMM.

## Related

`/home/al/Code/dler` (Rust) handles hoster links (Rapidgator etc.) via Real-Debrid. decypharr owns torrents/magnets/DMM/TorBox usenet and the mount. dler's `crates/realdebrid` may later be shared with a Rust port; see `~/Code/dler/docs/PROJECT_STATE.md`.

## Agents and skills

- **decypharr-dev** — project implementation owner: upstream PR imports, issue fixes, audit follow-ups, spec work, UI assets.
- Inherited globals: `stack-advisor` for stack/config questions, `website-auditor` for website audits.
- Skills: `git` for non-trivial repository state, commits and pushes; `rust-async-patterns` only once the port starts. No Go skill is installed; none is needed yet.
- Library docs: Context7 via `xd://mcp__context7_resolve_library_id` then `xd://mcp__context7_query_docs`; resolve real IDs, never invent them.
- Report unclear or wrong skill guidance with `append_feedback` (`.omp/tools/append-feedback.ts`).
