# Watchdog

- Anything touching upstream (push, PR, issue, comment) is a hard stop; `origin` is the only push target.
- Streaming and DFS reads are the hot path: watch body read/close concurrency (golang/go#81404), link refetch cooldowns, and RD/TorBox rate limits (`rate_limit` is a hard cap shared across API, repair and download-key calls).
- `pkg/manager` and `config.Get()` are shared hubs: flag changes that widen them instead of moving toward the audit §6 refactors.
- A config struct change without regenerated `fork/spec/` or an updated `env.md` breaks the guard tests; a fix without a failing-before test is incomplete.
- Security defaults from round 3 (WebDAV auth, TLS verify, Hearsay opt-in, secrets redaction, nonce CSP) must not regress.
- Keep `fork/merge-log.md`, `fork/PROJECT_STATE.md` and `fork/NEXT_STEPS.md` current after each round; flag claims about al's live stack that were not observed.
