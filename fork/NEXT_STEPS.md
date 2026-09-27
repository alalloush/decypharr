# Next steps

In order. Mark items done here and log merges in [merge-log.md](merge-log.md).

1. **Trial the fork on al's stack.** Back up `/opt/conf/decypharr` first. Build an image from `dev` (`BUILD_TAGS=nohearsay` optional), point `/opt/stacks/decypharr` at it, and work through the "Verify on the live setup" checklists in all three merge-log rounds. Check the deployment impacts: WebDAV now needs auth with `use_auth`, TLS verify is on, Hearsay is off, `rate_limit` is a hard cap, and secrets must be re-entered when their destination changes. Keep the upstream image tag to roll back.
2. **Remaining PRs.** #264; bundle splits per [research/bundles.md](research/bundles.md); usenet bundles last (al has no usenet providers).
3. **Audit follow-ups.** L10/L11 (refresh invalidation and cancellation) in [research/audit.md](research/audit.md); M6/M7 in [research/dfs-memory-disk.md](research/dfs-memory-disk.md).
4. **Spec.** OpenAPI from the chi routes; record the qBittorrent API calls Sonarr/Radarr make; provider fixtures through the `api_host` seam; a black-box conformance suite (planned in Bun, written only once al approves Bun/TS code here). See [spec/README.md](spec/README.md) and [research/port-plan.md](research/port-plan.md).
5. **Refactors** from [audit.md §6](research/audit.md#6-recommended-fork-refactors-before-the-port): inject config instead of `config.Get()`, split `pkg/manager`, detach DFS.
6. **UI optimisation** al wants, staying on server-rendered Go templates + Tailwind v4 + DaisyUI 5 + vanilla JS, with the nonce CSP and no inline handlers.
7. **Go 1.27.2 bump** once released (golang/go#81404); rerun the `internal/request` drain tests and `-race` suites on it.
