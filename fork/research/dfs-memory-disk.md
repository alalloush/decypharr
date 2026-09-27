# DFS RAM and disk budgets: audit M6 and M7

Status: evaluated and not fixed (2026-09-27). Both need a policy decision in `internal/buffer`, so neither is a clear, low-risk fix.

## M6: the RAM cap has a one-block floor per buffer

Verified:
- `Pool.shareFor` never returns less than one 1 MiB block (`internal/buffer/pool.go`).
- `Buffer.trimTo` never trims below one block either (`internal/buffer/buffer.go`).
- `Pool.reclaimMemory` gives up when a pass reclaims nothing.
- A released `CacheItem` keeps its buffer until the janitor closes it: 1 minute idle, with cleanup every 5 minutes.

So N retained buffers hold at least N MiB whatever `buffer_memory` says (default 512 MiB). The retained items also stay in `memDemand`, and every buffer's share is its `MemorySize` divided by that demand. During a scan, a stream that is still playing therefore shrinks to the one-block floor, and its read-ahead goes to disk.

Proposed fix:
1. Add `Buffer.SetIdle(bool)`. vfs calls it when an item's open count goes from zero to one and back, re-reading `opens` under a small mutex.
2. Idle disk-backed buffers leave `memDemand`. Changes to it go under `Pool.mu`, so they cannot race `Pool.remove`.
3. `reclaimMemory` trims idle disk-backed buffers first, down to zero blocks. Their dirty bytes are flushed to the sparse file first, so no data is lost.
4. The write path keeps the one-block floor, so a buffer that becomes active again can always allocate.

Risks:
- The demand accounting races described in step 2.
- Once idle buffers leave the demand, active buffers get larger shares, which changes RAM behaviour for every DFS user.

## M7: disk sizing assumes about four full-window streams

Re-traced:
- `vfs.NewCache` sets `backWindow = clamp(disk_cache_size/StreamDiskShare − read_ahead, 32 MiB, 256 MiB)`, with `StreamDiskShare = 4`.
- `reconcileReadAhead` clamps read-ahead to `disk_cache_size/4/2`.
- `Pool.reclaimDiskTo` only punches behind each read head beyond `backWindow`. It never touches read-ahead.

With the default 500 MB cache, each stream protects about 62.5 MB behind its head and 62.5 MB ahead. A fifth stream, or a scan whose released items still hold probe data, pushes the protected bytes past the limit. `reserveDisk` then returns `ErrDiskLimit` from `flushBlockLocked`. The eviction that needed the flush fails, so `writeRegion` fails and the downloader's write fails. The reader sees EIO after the retries.

Candidate fixes, none of them obviously right:
- **Scale `backWindow` by active buffers:** less seek-back cache for everyone once more than four streams run.
- **Drop the victim's unflushed bytes when eviction hits `ErrDiskLimit`:** this keeps the requested range and never fails the write. But a disk-backed buffer can then lose bytes a reader just checked with `HasRange`, and the reader gets `ErrNotPresent`, which is EIO today. It needs a re-download path for that race as well.
- **Let a write the reader is waiting on exceed the limit:** this breaks a cap the user set.

## For al's deployment

Size `mount.dfs.disk_cache_size` for real concurrency: about `(concurrent streams + scanner slack) × (256 MiB + read_ahead)`. For example, 10–20 GB instead of the 500 MB default. The back-window stops growing at 256 MiB, so a larger cache mostly buys more concurrent streams.
