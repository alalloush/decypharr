---
title: Torbox Setup
description: Configure Torbox provider.
---

Torbox is a supported Debrid provider.

## Configuration

```json
{
  "debrids": [
    {
      "provider": "torbox",
      "name": "Torbox",
      "api_key": "YOUR_API_KEY"
    }
  ]
}
```

Get your API key from the Torbox dashboard.

All configuration options from [Real Debrid](./real-debrid/) apply (rate limits, proxy, etc.).

## How Decypharr uses TorBox

- **Download links.** Decypharr resolves each file's CDN URL once with `requestdl` and reuses it for every range read. A URL is kept for at most one hour, or for `auto_expire_links_after` if that is shorter. When the CDN rejects a URL (400/403/404/410), Decypharr fetches a replacement. If a file keeps failing, Decypharr asks TorBox for a new link at most once every five minutes, until the file validates again.
- **Uncached releases.** Decypharr checks TorBox's cache before it adds a torrent. With `download_uncached: false`, an uncached release is refused at once as `torrent_not_cached`, which reaches Sonarr/Radarr as HTTP 404. TorBox answers cache checks from its own hour-long cache, so a release that was cached less than an hour ago can still be refused until that cache entry expires. With `download_uncached: true`, TorBox allows 60 uncached adds per API key every hour; Decypharr refuses the next one until the hour allows it, so the add moves on to your next provider or fails right away.
- **Rate limit.** TorBox allows 300 API requests per minute per API key. Decypharr keeps each key at or under 288 per minute, counting list refreshes, submissions and link requests together; a download key other than `api_key` has its own budget. See [Rate limits](../configuration/#rate-limits).
- **Usenet (Pro plan).** On a Pro account, finished TorBox usenet downloads are listed next to torrents and can be streamed and symlinked. Decypharr treats them as read-only. Removing such an entry in Decypharr does not delete it on TorBox, and the entry comes back on the next refresh; delete it from the TorBox dashboard instead.

See [Configuration Reference](../configuration/#debrid-providers) for full options.
