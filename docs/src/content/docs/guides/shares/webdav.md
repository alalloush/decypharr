---
title: WebDAV Server
description: Access files via WebDAV protocol.
---

Decypharr includes a WebDAV server for browsing and streaming files without mounting.

The library tree, virtual folders, and folder naming are the same for every share. See
[Shares Overview](../overview/).

## Access WebDAV

**URL**: `http://decypharr:8282/webdav/`

## Mount in OS

### macOS

**Finder** → **Go** → **Connect to Server** (`Cmd+K`)

```
http://decypharr:8282/webdav/
```

Enter username and password.

### Windows

**File Explorer** → **This PC** → **Map network drive**

```
\\decypharr@8282\DavWWWRoot\webdav\
```

### Linux

```bash
# Install davfs2
sudo apt install davfs2

# Mount
sudo mount -t davfs http://decypharr:8282/webdav /mnt/decypharr

# With auth
sudo mount -t davfs -o username=USER,password=PASS \
  http://decypharr:8282/webdav /mnt/decypharr
```

## Authentication

WebDAV follows `use_auth`, like the web UI and the API. While it is on, every request needs one of:

- the web UI username and password, as Basic auth;
- the API token, as the Basic auth password (any username) or in an `Authorization: Bearer` header. In
  [token-only mode](../../configuration/#token-only-authentication) this is the only way in.

Without them the server answers `401` with a Basic auth challenge. With `use_auth` off, anyone who can reach the
port can read the library. There is no separate WebDAV switch: turning `use_auth` off opens the web UI and the API
too.

A changed password or a refreshed API token applies at once; a client still sending the old one gets `401`.

The built-in [rclone mount](../../mounting/rclone-internal/) authenticates by itself, with a token derived from the
session secret, so refreshing the API token does not affect it. An
[external rclone remote](../../mounting/rclone-external/) needs `user` and `pass`, or `bearer_token` set to the API
token.

`enable_webdav_auth` no longer exists. A config that still has it loads normally and drops the key at the next save.

## Read-only by default

A `DELETE` of a torrent folder, or of its last file, deletes the torrent from the debrid provider. WebDAV refuses it
with `403` unless `webdav_allow_delete` is set (or `DECYPHARR_WEBDAV_ALLOW_DELETE=true`):

```json
{
  "webdav_allow_delete": true
}
```

The setting applies without a restart.

`COPY` and `MOVE` always get `405 Method Not Allowed`: the tree mirrors your debrid accounts, so there is nowhere to
put a copy and nothing to rename. Uploads and other changes (`PUT`, `MKCOL`, `PROPPATCH`, `LOCK`) get `405` as well.
The `Allow` header lists what works: `OPTIONS, GET, HEAD, PROPFIND`, plus `DELETE` when `webdav_allow_delete` is set.

## Browsers

WebDAV sends no CORS headers, so a web page on another site cannot read your library or send deletes through a
visitor's browser. Native WebDAV clients (rclone, Infuse, Finder, Explorer, davfs2) do not use CORS and are not
affected. A browser-based WebDAV client served from another origin no longer works.

## Streaming

WebDAV supports HTTP Range requests for streaming:

```bash
# Direct playback
vlc http://decypharr:8282/webdav/__all__/TorrentName/video.mkv
```

Provide username:password if auth is enabled (or any username and the API token):

```bash
vlc http://user:pass@decypharr:8282/webdav/__all__/TorrentName/video.mkv
```

## STRM Files

Create STRM files pointing to WebDAV URLs:

```
http://decypharr:8282/webdav/sonarr/ShowName/S01E01.mkv
```

When Plex/Jellyfin plays the STRM, it streams from WebDAV. With `use_auth` on, such a URL needs the credentials in it,
like the `vlc` example above. The STRM files Decypharr writes itself point at signed `/stream/...` URLs instead, which
play without credentials.

## Performance

WebDAV streams directly from Debrid/Usenet. It does not use the [share cache](../overview/#share-cache)
— that cache serves NFS and SMB only. Performance depends on:

- Debrid provider speed
- Network bandwidth
- Client buffer settings

For best performance, use [DFS mounting](../../mounting/dfs/) instead of WebDAV.

## Troubleshooting

### Connection Refused

- Verify Decypharr is running: `curl http://decypharr:8282/version`
- Check firewall rules

### Authentication Failed

- Use the web UI username and password, or the API token as the password
- WebDAV asks for credentials exactly when `use_auth` is on

### Slow Playback

WebDAV has no local cache. Consider:

- Using DFS/Rclone mount instead
- Increasing client buffer size
- Checking Debrid provider performance

## Security

:::caution
Basic auth sends credentials base64-encoded, not encrypted. Put WebDAV behind HTTPS when it leaves your network:
:::

```nginx
# nginx reverse proxy
location /webdav/ {
    proxy_pass http://decypharr:8282/webdav/;
    proxy_set_header Authorization $http_authorization;
    proxy_pass_header Authorization;
}
```
