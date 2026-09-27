# Environment overrides

Every `DECYPHARR_*` variable that `internal/config` reads through `getEnv`, with the `config.json` path it sets. `TestEnvDocMatchesCode` (`internal/config/env_doc_test.go`) fails when a key read in code is missing here, or when this file lists a key that nothing reads. Array indexes are written as `N`.

Behaviour as of upstream `beta` 249ac9e plus this fork:

- Names are `DECYPHARR_` plus the JSON path in upper case, with `__` between levels and array indexes. Some keys differ from their JSON path (`RCLONE__*` sets `mount.rclone.*`; `RCLONE__RC_PORT` sets `mount.rclone.port`).
- An empty value is the same as unset.
- Booleans are true for `true`, `1` or `yes`; any other value is false.
- Numbers that fail to parse are ignored silently, and the file value stays.
- Overrides are applied when the config loads, after defaults and on top of `config.json`. They are not applied on the very first start, when `config.json` does not exist yet and is created; they take effect from the next start.
- Overrides are in-memory, but any later save (settings saved from the web UI, or a load that has to write new signing secrets) writes the overridden values, secrets included, into `config.json`.
- Defaults run before the overrides. A debrid entry that exists only in the environment therefore misses the per-debrid defaults on that start (`provider` falling back to `name`, `download_api_keys` falling back to `api_key`, `workers`, refresh intervals) until something saves the config.

## Server and auth

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_PORT` | `port` | string | |
| `DECYPHARR_BIND_ADDRESS` | `bind_address` | string | |
| `DECYPHARR_URL_BASE` | `url_base` | string | |
| `DECYPHARR_LOG_LEVEL` | `log_level` | string | |
| `DECYPHARR_USE_AUTH` | `use_auth` | bool | |
| `DECYPHARR_AUTH_TOKEN_ONLY` | `auth.json` `token_only` | bool | Only when auth is enabled. Meant to turn on token-only auth for headless installs. At 249ac9e it has no effect: `Config.GetAuth` returns a copy, so the change is discarded. |
| `DECYPHARR_API_TOKEN` | `auth.json` `api_token` | string, secret | Same as `AUTH_TOKEN_ONLY`: only with auth enabled, and discarded at 249ac9e. |
| `DECYPHARR_SECRET_KEY` | (`session_secret`) | string, secret | Not written into the config. When set, it replaces `session_secret` as the session signing key each time the key is read. |

## Downloads and manager

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_DOWNLOAD_FOLDER` | `download_folder` | string | |
| `DECYPHARR_REFRESH_INTERVAL` | `refresh_interval` | string | Duration such as `30s`. |
| `DECYPHARR_MAX_ACTIVE_DOWNLOADS` | `max_active_downloads` | int | |
| `DECYPHARR_SKIP_PRE_CACHE` | `skip_pre_cache` | bool | |
| `DECYPHARR_ALWAYS_RM_TRACKER_URLS` | `always_rm_tracker_urls` | bool | |
| `DECYPHARR_MIN_FILE_SIZE` | `min_file_size` | string | Size such as `100MB`. |
| `DECYPHARR_MAX_FILE_SIZE` | `max_file_size` | string | Size such as `50GB`. |
| `DECYPHARR_REMOVE_STALLED_AFTER` | `remove_stalled_after` | string | Duration. |
| `DECYPHARR_ENABLE_WEBDAV_AUTH` | `enable_webdav_auth` | bool | |
| `DECYPHARR_RETRIES` | `retries` | int | |
| `DECYPHARR_SKIP_AUTO_MOVE` | `skip_auto_move` | bool | |
| `DECYPHARR_CATEGORIES__N` | `categories[N]` | string | N from 0 to 99. Reading stops at the first unset index. |
| `DECYPHARR_ALLOWED_FILE_TYPES__N` | `allowed_file_types[N]` | string | N from 0 to 99. Reading stops at the first unset index. |
| `DECYPHARR_NZB_USER_AGENT` | `nzb_user_agent` | string | |

## Debrid providers

Indexes 0 to 9. The other keys of an index apply only when that index's `NAME` is set; `PROVIDER` alone does nothing. An index beyond the file's array appends entries, and an index inside it overwrites fields of the existing entry.

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_DEBRIDS__N__NAME` | `debrids[N].name` | string | Required for the other keys of this index. |
| `DECYPHARR_DEBRIDS__N__PROVIDER` | `debrids[N].provider` | string | `realdebrid`, `alldebrid`, `debridlink`, `torbox` or `premiumize`. |
| `DECYPHARR_DEBRIDS__N__API_KEY` | `debrids[N].api_key` | string, secret | |
| `DECYPHARR_DEBRIDS__N__FOLDER` | `debrids[N].folder` | string | Deprecated field. |
| `DECYPHARR_DEBRIDS__N__PROXY` | `debrids[N].proxy` | string | HTTP(S) or `socks5://` proxy URL. |
| `DECYPHARR_DEBRIDS__N__API_HOST` | `debrids[N].api_host` | string | Fork addition. API base URL override (scheme, host, version path) for fake providers in tests. Empty keeps the provider's public API. |
| `DECYPHARR_DEBRIDS__N__PRIORITY` | `debrids[N].priority` | int | Fork addition (#294). Lower is tried first; ties keep config order. `0` means config position (N+1). Ignored when not an integer. |
| `DECYPHARR_DEBRIDS__N__KEEP_IN_SYNC` | `debrids[N].keep_in_sync` | bool | Fork addition (upstream #275, part a). Adopts finished provider torrents that nothing owns yet as completed downloads in category `other`. |

## Arr applications

Indexes 0 to 19. The other keys apply only when that index's `NAME` is set.

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_ARRS__N__NAME` | `arrs[N].name` | string | Required for the other keys of this index. |
| `DECYPHARR_ARRS__N__HOST` | `arrs[N].host` | string | |
| `DECYPHARR_ARRS__N__TOKEN` | `arrs[N].token` | string, secret | |

## Mount

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_MOUNT__DFS__CACHE_DIR` | `mount.dfs.cache_dir` | string | |
| `DECYPHARR_MOUNT__DFS__CHUNK_SIZE` | `mount.dfs.chunk_size` | string | Size. |
| `DECYPHARR_MOUNT__DFS__READ_AHEAD_SIZE` | `mount.dfs.read_ahead_size` | string | Size. |
| `DECYPHARR_MOUNT__DFS__CACHE_EXPIRY` | `mount.dfs.cache_expiry` | string | Duration. |
| `DECYPHARR_MOUNT__DFS__DISK_CACHE_SIZE` | `mount.dfs.disk_cache_size` | string | Size. |
| `DECYPHARR_MOUNT__DFS__CACHE_CLEANUP_INTERVAL` | `mount.dfs.cache_cleanup_interval` | string | Duration. |
| `DECYPHARR_MOUNT__DFS__DAEMON_TIMEOUT` | `mount.dfs.daemon_timeout` | string | Duration. |
| `DECYPHARR_MOUNT__DFS__FUSE_MAX_BACKGROUND` | `mount.dfs.fuse_max_background` | int | |
| `DECYPHARR_MOUNT__DFS__FUSE_MAX_READ_AHEAD` | `mount.dfs.fuse_max_read_ahead` | string | Size. |
| `DECYPHARR_MOUNT__DFS__UID` | `mount.dfs.uid` | uint32 | |
| `DECYPHARR_MOUNT__DFS__GID` | `mount.dfs.gid` | uint32 | |
| `DECYPHARR_MOUNT__DFS__UMASK` | `mount.dfs.umask` | string | |
| `DECYPHARR_RCLONE__RC_PORT` | `mount.rclone.port` | string | |
| `DECYPHARR_RCLONE__LOG_LEVEL` | `mount.rclone.log_level` | string | |
| `DECYPHARR_RCLONE__VFS_CACHE_MODE` | `mount.rclone.vfs_cache_mode` | string | `off`, `minimal`, `writes` or `full`. |
| `DECYPHARR_RCLONE__CACHE_DIR` | `mount.rclone.cache_dir` | string | |
| `DECYPHARR_RCLONE__TRANSFERS` | `mount.rclone.transfers` | int | |

## NFS and SMB exports

The export defaults are re-applied after these overrides.

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_NFS__ENABLED` | `nfs.enabled` | bool | |
| `DECYPHARR_NFS__BIND_ADDRESS` | `nfs.bind_address` | string | |
| `DECYPHARR_NFS__PORT` | `nfs.port` | uint16 | |
| `DECYPHARR_NFS__ALLOWED_NETWORKS` | `nfs.allowed_networks` | list | Split on commas, spaces and newlines. Replaces the file list. |
| `DECYPHARR_SMB__ENABLED` | `smb.enabled` | bool | |
| `DECYPHARR_SMB__BIND_ADDRESS` | `smb.bind_address` | string | |
| `DECYPHARR_SMB__PORT` | `smb.port` | uint16 | |
| `DECYPHARR_SMB__SHARE_NAME` | `smb.share_name` | string | |
| `DECYPHARR_SMB__USERNAME` | `smb.username` | string | |
| `DECYPHARR_SMB__PASSWORD` | `smb.password` | string, secret | |
| `DECYPHARR_SMB__REQUIRE_SIGNING` | `smb.require_signing` | bool | |
| `DECYPHARR_SMB__ALLOWED_NETWORKS` | `smb.allowed_networks` | list | Split on commas, spaces and newlines. Replaces the file list. |
| `DECYPHARR_SHARE_CACHE__ENABLED` | `share_cache.enabled` | bool | |
| `DECYPHARR_SHARE_CACHE__DIR` | `share_cache.dir` | string | |
| `DECYPHARR_SHARE_CACHE__MAX_SIZE` | `share_cache.max_size` | string | Size. |
| `DECYPHARR_SHARE_CACHE__MAX_AGE` | `share_cache.max_age` | string | Duration. |
| `DECYPHARR_SHARE_CACHE__CHUNK_SIZE` | `share_cache.chunk_size` | string | Size. |
| `DECYPHARR_SHARE_CACHE__READ_AHEAD` | `share_cache.read_ahead` | string | Size. |

## Usenet

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_USENET__MAX_CONNECTIONS` | `usenet.max_connections` | int | Also sets `usenet.processing_max_connections` unless `USENET__PROCESSING_MAX_CONNECTIONS` is set. |
| `DECYPHARR_USENET__PROCESSING_MAX_CONNECTIONS` | `usenet.processing_max_connections` | int | |
| `DECYPHARR_USENET__READ_AHEAD` | `usenet.read_ahead` | string | Size. |
| `DECYPHARR_USENET__BODY_PIPELINE_DEPTH` | `usenet.body_pipeline_depth` | int | Clamped by `NormalizeBodyPipelineDepth`. |
| `DECYPHARR_USENET__STREAM_BACKUP_WAIT` | `usenet.stream_backup_wait` | string | Duration. |
| `DECYPHARR_USENET__SOCKET_READ_BUFFER` | `usenet.socket_read_buffer` | string | Size. |
| `DECYPHARR_USENET__SOCKET_WRITE_BUFFER` | `usenet.socket_write_buffer` | string | Size. |
| `DECYPHARR_USENET__PROCESSING_TIMEOUT` | `usenet.processing_timeout` | string | Duration. |
| `DECYPHARR_USENET__AVAILABILITY_SAMPLE_PERCENT` | `usenet.availability_sample_percent` | int | |
| `DECYPHARR_USENET__IMPORT_AVAILABILITY_SAMPLE_PERCENT` | `usenet.import_availability_sample_percent` | int | |
| `DECYPHARR_USENET__DISK_PATH` | `usenet.disk_path` | string | |

Providers use indexes 0 to 9. The other keys of an index apply only when that index's `HOST` is set.

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_USENET__PROVIDERS__N__HOST` | `usenet.providers[N].host` | string | Required for the other keys of this index. |
| `DECYPHARR_USENET__PROVIDERS__N__PORT` | `usenet.providers[N].port` | int | |
| `DECYPHARR_USENET__PROVIDERS__N__USERNAME` | `usenet.providers[N].username` | string | |
| `DECYPHARR_USENET__PROVIDERS__N__PASSWORD` | `usenet.providers[N].password` | string, secret | |
| `DECYPHARR_USENET__PROVIDERS__N__BACKBONE` | `usenet.providers[N].backbone` | string | |
| `DECYPHARR_USENET__PROVIDERS__N__MAX_CONNECTIONS` | `usenet.providers[N].max_connections` | int | |
| `DECYPHARR_USENET__PROVIDERS__N__SSL` | `usenet.providers[N].ssl` | bool | |
| `DECYPHARR_USENET__PROVIDERS__N__PRIORITY` | `usenet.providers[N].priority` | int | |
| `DECYPHARR_USENET__PROVIDERS__N__BACKUP` | `usenet.providers[N].backup` | bool | |

## Hearsay

| Variable | Config path | Type | Notes |
|---|---|---|---|
| `DECYPHARR_HEARSAY__DISABLED` | `hearsay.disabled` | bool | |
| `DECYPHARR_HEARSAY__PARTICIPATE` | `hearsay.participate` | bool | |
| `DECYPHARR_HEARSAY__PUBLISH` | `hearsay.publish` | bool | |
| `DECYPHARR_HEARSAY__ADVICE_MODE` | `hearsay.advice_mode` | string | Trimmed and lower-cased. |
| `DECYPHARR_HEARSAY__MIN_SUPPORT` | `hearsay.min_support` | float | |
| `DECYPHARR_HEARSAY__MIN_EVIDENCE` | `hearsay.min_evidence` | float | |
| `DECYPHARR_HEARSAY__MIN_SOURCES` | `hearsay.min_sources` | int | |
| `DECYPHARR_HEARSAY__PORT` | `hearsay.port` | int | |
| `DECYPHARR_HEARSAY__GOSSIP_PORT` | `hearsay.gossip_port` | int | |
| `DECYPHARR_HEARSAY__INTERVAL` | `hearsay.interval` | string | Duration. |
| `DECYPHARR_HEARSAY__MAX_STORAGE_BYTES` | `hearsay.max_storage_bytes` | int64 | |
| `DECYPHARR_HEARSAY__MAX_FEEDS_PER_NAMESPACE` | `hearsay.max_feeds_per_namespace` | int | |
| `DECYPHARR_HEARSAY__MAX_SEEDED_TORRENTS` | `hearsay.max_seeded_torrents` | int | |
| `DECYPHARR_HEARSAY__FOLLOW` | `hearsay.follow` | list | Comma-separated; blanks dropped. Replaces the file list. |

## Other environment variables

These are read outside `internal/config` and are not config overrides.

| Variable | Read in | Effect |
|---|---|---|
| `DECYPHARR_FIX_NZB_SIZES` | `pkg/manager/manager.go` | `1` runs the NZB size fix-up at startup. |
| `UMASK` | `cmd/decypharr/main.go` | Process umask, octal. An invalid value stops startup. |
| `ENABLE_PPROF` | `main.go` | Any value enables pprof, like the command-line flag. |
| `DFS_FUSE_BACKEND` | `pkg/mount/dfs/backend/interface.go` | FUSE backend for the DFS mount on Linux; default `hanwen`. |
| `QBIT_PORT` | `cmd/healthcheck/main.go` | Port the healthcheck probes; defaults to the config `port`. |
