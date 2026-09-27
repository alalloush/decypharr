# al's decypharr fork

Base: upstream `beta` `249ac9e` (2026-09-16). Working branch: `dev`. Remotes: `private` = git.por.re:al/decypharr (where work is pushed), `upstream` = sirrobot01/decypharr (read-only; nothing is posted upstream), `github` = alalloush/decypharr (pushes disabled).

Plan: merge open upstream PRs and fix issues, Real-Debrid and TorBox first. Alongside that work, build the language-neutral spec in `fork/spec/`. A later port is verified against this Go fork. No Rust or Bun code for now.

- `research/issues.md`: open-issue triage (71 issues, 2026-09-26).
- `research/prs.md`: open-PR triage and merge order (63 PRs).
- `research/bundles.md`: verdicts on the large feature PRs #350, #389 and #392 (not merged).
- `research/dmm.md`: DMM integration options (these stay in Go).
- `research/port-plan.md`: spec artifacts, port scope and measurements.
- `spec/`: config JSON Schema and `DECYPHARR_*` env list; see `spec/README.md`.

Merged PRs keep their original author; fork commits use the `omp` identity.

Defaults that differ from upstream:

- Hearsay joins the public P2P network only with `hearsay.participate: true` (audit H4); upstream joins unless it is `false`. The `nohearsay` build tag (`BUILD_TAGS=nohearsay` for the Dockerfile) leaves Hearsay out of the binary.
- A debrid entry's API, repair and download-key calls share one `rate_limit` budget, and retries count against it (audit M11). Upstream gives each call path its own limiter and none without `rate_limit`. Real-Debrid stays at or under 240 requests per minute across the whole process and TorBox at or under 288 per minute per API key, whatever `rate_limit` says; other providers default to 250 per minute. TorBox refuses uncached adds past 60 per key per hour. `debrids[].workers` is gone; it was never read.

Toolchain: `go.mod` requires go1.26.6, and the Dockerfile's `golang:1.26-alpine` pulls the latest 1.26.x. Go 1.27.1 is held back because of [golang/go#81404](https://github.com/golang/go/issues/81404): an HTTP/1 deadlock when a response body is read and closed concurrently, a regression from 1.27's automatic body draining. Streaming is decypharr's hot path. Move to go1.27.2 or later once it is released; it carries the fix ([CL 830424](https://go.dev/cl/830424)). sonic v1.15.4 already runs natively on 1.27, and the `internal/request` drain tests pass on both.
