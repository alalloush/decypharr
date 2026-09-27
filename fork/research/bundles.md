# Large upstream feature bundles: verdicts (round 2, 2026-09-27)

Three open upstream PRs are too big to merge as one commit. Each entry below says what the bundle does, which parts are worth keeping, how to split it, the risks, and how much it matters for al's setup (Real-Debrid + TorBox Pro, DFS mount, Sonarr/Radarr through the qBit API, no usenet, reverse proxy, and torrents added directly through DMM). The base for all file references is `dev` after round 1, which is upstream `beta` 249ac9e plus the round-1 merges. None of the three is merged.

## #350: ffprobe validation in repair

[PR #350](https://github.com/sirrobot01/decypharr/pull/350) by @TheMightyBattleCat, +982/−39, 3 commits.

**What it does**
- `fe4b6f3`: new `repair.ffprobe_check` (plus `ffprobe_timeout` and `ffprobe_path`). When a repair-sweep file passes the provider/NNTP probe, it also runs `ffprobe` against decypharr's own WebDAV endpoint. The file counts as broken if it has no streams, no video, no duration, or an absurd duration. Code: new `pkg/manager/ffprobe.go`, hooked into `probeFile`.
- `887ac4c`: new `repair.ffprobe_on_import`. Before a download is reported complete, video files of 100 MiB or more are probed. An entry is rejected if a file fails twice, so the arr blocklists the release and grabs another. Code: `completeEntry` in `pkg/manager/downloader.go`.
- `9bc4503`: pulls expected runtimes from Sonarr/Radarr (`pkg/arr/content.go`, `types.go`) and compares them with the probed duration using wide tolerance bands. A "too short" verdict also requires an unreadable tail. This fixes an endless-replace loop, but that loop only exists once the PR's own runtime check is on. On HEAD, repair never compares duration with arr metadata.

**Conflicts with HEAD.** The PR targets the old `pkg/manager/repair_sweep.go`, and repair now lives in `pkg/repair/` (`sweep.go`, `entry.go`, `torrent.go`). The sweep hook must be re-implemented in `pkg/repair`; it will not cherry-pick. `completeEntry` in `downloader.go` still has the same shape, but it is a busy file in round 2 (#394 and #410 are going there). The PR also adds a WebDAV bearer-token bypass in `pkg/server/webdav/handler.go` and new config/UI fields. None of this is on HEAD.

**What's valuable.** It catches files whose link is alive but whose content cannot be played: truncated uploads, broken containers, releases with no video stream. The provider probe cannot see any of that. The import gate could stop bad grabs before they reach the library.

**Split, in order** (every piece off by default):
1. The ffprobe checker plus the stream/duration verdicts, wired into `pkg/repair` for torrent files. It needs the file filter the import gate already has (video extension and a minimum size). The sweep path in the PR probes every file that passed its provider check, so an audio-only file allowed by the default `allowed_file_types` (mp3, flac, …) fails with `no_video_stream`. With auto-repair on, that triggers a re-insert. Tests: a healthy fixture, a truncated fixture, and a non-video file that must be skipped.
2. Arr runtime mapping plus the `9bc4503` tail check. This depends on step 1 and is useless without it.
3. The import gate as its own change, with end-to-end tests of the reject and blocklist path.
4. Packaging: the `Dockerfile` does not install ffmpeg/ffprobe, so in the standard image both options only log a warning and do nothing. Add the package, or document that it is missing.

**Risks**
- **Cost.** Each probe reads real byte ranges through WebDAV, and those reads go to the debrid CDN. The worst case per file is a 90 s timeout, two attempts, and a tail probe. Across repair workers × files per entry, that adds CDN traffic and API link churn during sweeps.
- **False "broken" verdicts.** Any ffprobe or WebDAV error other than a timeout, such as a transient 401/404 or a read error, is treated as unreadable. Two failed attempts then mark a good file broken, and with auto-repair on, it is replaced.
- **Security.** The internal bearer token passes the whole WebDAV auth middleware for every method, not just GET, and it sits on ffprobe's command line, where the process list shows it. Limit it to read-only methods, or have ffprobe read from a loopback-only handler.

**al-relevance: medium.** Detecting unplayable RD/TorBox files and gating imports would help him. The usenet half of the motivation (STAT-alive/BODY-dead articles) does not apply. The bandwidth cost and the false-positive re-insert risk land directly on his accounts.

**Verdict:** don't merge. If wanted, port piece 1 (filtered, off by default, tested) and piece 2 together into `pkg/repair`. Defer the import gate until the image ships ffprobe and the rejection flow is proven.

## #392: arr webhook cleanup and managed-only mode

[PR #392](https://github.com/sirrobot01/decypharr/pull/392) by @buzzromain, +1909/−31, 2 commits.

**What it does**
- `d9c8ff7`: per-file arr reference tracking in a new `arr_refs` store (`pkg/storage/arr_files.go`), backfilled at startup from arr import history (`syncArrFiles`). A new `/webhooks/arr` endpoint handles Download, Rename, EpisodeFile/MovieFile delete, and Series/Movie delete. With the new per-arr `allow_delete`, the debrid torrent is deleted once no tracked file references its hash anymore; upgrades count as deletions.
- `d9d2f88`: `managed_only` (env `DECYPHARR_MANAGED_ONLY`). Provider sync ignores remote torrents decypharr did not add. Managed torrents that vanish from the provider are re-submitted, at most 3 attempts with a 5-minute cooldown. It also adds a Maintenance tab with local-DB purge and provider purge; provider purge deletes every provider torrent decypharr doesn't know.

**Conflicts with HEAD.** Every touched path still exists in the same shape: sync in `pkg/manager/torrent.go` (also edited by #399 in this round), `manager.go` startup, store setup in `pkg/storage/storage.go`, the `Arr` config struct, `arr.New` (its signature changes, so every caller moves), `pkg/server/server.go` webhook registration, and protected routes. There is no functional overlap with round-1 #366 beyond sitting next to the Tautulli webhook.

**What's valuable.** Removing debrid torrents once the arr has deleted or upgraded the file it imported from them. That keeps the account (and the DFS listing) from filling up with dead torrents. Reference tracking is the right base for this.

**Split, in order:**
1. The arr reference store plus history backfill. Read-only toward arrs and providers.
2. The webhook endpoint and registration. Before this can land:
   - Move the route out of the `authMiddleware` group. The PR registers it next to `/webhooks/tautulli` inside that group (`pkg/server/server.go`), so with `use_auth` on, the arr gets 401 before the handler's token check runs.
   - Replace the `?token=<arr API key>` query parameter. Round 1 refused query-string tokens (#366) because they leak into proxy and access logs, and this one is the arr's own API key.
   - Stop registering unconditionally. As written, `RegisterArrWebhooks` runs at every startup and writes a notification into every configured Sonarr/Radarr instance. That changes default behaviour; make it opt-in.
3. Optional debrid deletion via `allow_delete`, as its own review with tests for multi-file torrents, season packs shared by several episodes, and upgrades.
4. `managed_only` and the Maintenance purge last, if at all.

**Risks**
- `normalizePath` lowercases paths (`filepath.Clean(strings.ToLower(p))`). On Linux, paths that differ only by case collide, so one file's reference can overwrite another's, and a hash can look unreferenced and get deleted.
- The reference scan skips malformed records without a warning, with the same effect.
- Retry state for re-submission lives in memory, so a restart resets the attempt limit. Sync also returns early on an empty remote list (`torrent.go`), so re-submission never runs when the provider reports nothing.
- Provider purge is irreversible, and it works from a scan snapshot that can go stale between preview and execute.

**al-relevance: split.** Arr-driven cleanup: medium to high. He runs Sonarr/Radarr against RD/TorBox, and dead torrents accumulate. `managed_only` and provider purge: negative. He adds torrents directly through DMM, so managed-only would hide them from the mount, and provider purge would delete them. This also works against the DMM import plan in `research/dmm.md`.

**Verdict:** don't merge. Port pieces 1–3 after fixing webhook routing, token transport, opt-in registration and case-sensitive keys. Skip `managed_only` and provider purge unless DMM-added torrents are explicitly exempt.

## #389: .torrent-first submission

[PR #389](https://github.com/sirrobot01/decypharr/pull/389) by @buzzromain, +678/−91, 10 commits.

**What it does**
- Per-provider `use_torrent_file` (`*bool`, nil means true; `internal/config/debrid.go`), plus UI and docs. When the grab arrived as a `.torrent`, upload the file instead of a magnet. It adds upload paths for TorBox (multipart `createtorrent`) and DebridLink, and gates the existing AllDebrid/Real-Debrid file paths.
- Tracker stripping moves from intake (`GetMagnetFromFile`/`GetMagnetFromUrl`, qBit, API) to the moment of submission. The request's `always_rm_tracker_urls` choice is persisted on the entry as a new `storage.proto` field 40 (`RmTrackerUrls`). `stripTrackersFromTorrentFile` rewrites the `.torrent` bytes themselves.
- `.torrent` bytes are stored as a per-hash sidecar (`pkg/storage/torrent_files.go`) and reused by repair re-insertion (`fixer.go`) and rebuilt queue jobs.

**What HEAD already does** (checked): Real-Debrid (`realdebrid.go:393`), AllDebrid (`alldebrid.go:166`) and Premiumize (`premiumize.go:154`) already upload the `.torrent` file whenever the grab was one (`Magnet.IsTorrent()`). So "default true" is new only for TorBox and DebridLink, and the PR's real new knob for RD/AD/PM is `use_torrent_file=false`.

HEAD also has a privacy bug the PR fixes along the way. `always_rm_tracker_urls` strips trackers from the magnet string only (`GetMagnetFromBytes`), while the uploaded `.torrent` bytes (`Magnet.File`) still carry their announce URLs to RD/AD/PM.

**Conflicts with HEAD**
- TorBox `SubmitMagnet` now starts with round-1 #370's cache check when `download_uncached=false`. The PR's upload path must go through that check, and through the submission rate lane, instead of calling `tb.client` directly.
- `Storage.Delete` (`pkg/storage/entry.go`) has changed shape, so sidecar cleanup must be placed by hand.
- `storage.pb.go` was generated with protoc-gen-go 1.36.7 / protoc 3.21.12; HEAD uses 1.36.11 / 6.32.0. Regenerate it with the repo's own toolchain rather than taking the PR's file.
- The intake helpers change signature, so every qBit/API caller moves.

**Split, in order:**
1. **Strip trackers from uploaded `.torrent` bytes when `always_rm_tracker_urls` is on.** This is a small fork-sized fix for the HEAD bug above. Test: parse the uploaded bytes and assert there is no `announce`/`announce-list`.
2. Move stripping to submission time and persist the per-request choice (field 40). This only matters once bytes are persisted (step 4), because re-insertion then needs to know the original choice.
3. TorBox `.torrent` upload behind `use_torrent_file`, off by default at first. Its request tests must pin the multipart field name and `add_only_if_cached`. That API contract is unverified [INFERENCE], and the PR ignores multipart writer errors.
4. The `.torrent` sidecar for re-insertion. The PR saves it before the queue insert, so a failed insert orphans the file. Sidecars run from tens of KB to a few MB for large packs (piece hashes), and nothing garbage-collects them apart from `Delete`.
5. Flip the default on only after 3 has run live.

**Risks**
- The default flip changes TorBox submissions for every user who grabs `.torrent` files.
- Protobuf field 40 is wire-compatible (old rows read as false), but an older binary rewriting an entry drops the flag.
- Re-insertion silently falls back to a magnet when the sidecar is missing.

**al-relevance: medium.** Sonarr/Radarr grabs of `.torrent` files hit this path on both of his providers. Piece 1 is a real privacy fix for his RD uploads if he enables `always_rm_tracker_urls`. TorBox file upload could help uncached or DHT-poor releases, but his TorBox use is mostly cached content, where a magnet already works. DMM-added torrents never pass through this path.

**Verdict:** don't merge. Take piece 1 as a small fork fix next round. Consider pieces 2–4 as a unit if repair re-insertions of `.torrent` grabs fail in practice. Leave the TorBox default off.
