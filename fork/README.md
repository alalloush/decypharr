# al's decypharr fork

Base: upstream `beta` `249ac9e` (2026-09-16). Working branch: `dev`. Remotes: `origin` = alalloush/decypharr (fork), `upstream` = sirrobot01/decypharr (read-only; nothing is posted upstream).

Plan: merge open upstream PRs and fix issues, Real-Debrid and TorBox first. Alongside that work, build the language-neutral spec in `fork/spec/`. A later port is verified against this Go fork. No Rust or Bun code for now.

- `research/issues.md`: open-issue triage (71 issues, 2026-09-26).
- `research/prs.md`: open-PR triage and merge order (63 PRs).
- `research/bundles.md`: verdicts on the large feature PRs #350, #389 and #392 (not merged).
- `research/dmm.md`: DMM integration options (these stay in Go).
- `research/port-plan.md`: spec artifacts, port scope and measurements.
- `spec/`: config JSON Schema and `DECYPHARR_*` env list; see `spec/README.md`.

Merged PRs keep their original author; fork commits use the `omp` identity.
