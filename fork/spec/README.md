# Spec

Language-neutral description of the Go fork. A later port is generated from and checked against these files. See `../research/port-plan.md` for the full list of planned artifacts.

## Files

| File | Source | Kept in sync by |
|---|---|---|
| `config.schema.json` | Generated from the `internal/config` structs by `cmd/schemagen` | `TestSchemaUpToDate` (`cmd/schemagen/main_test.go`) fails when the file is stale |
| `env.md` | Hand-written list of every `DECYPHARR_*` override, with its config path and parsing rules | `TestEnvDocMatchesCode` (`internal/config/env_doc_test.go`) fails when a key read by `getEnv` is missing, or when a listed key is no longer read |

## Config schema

`config.schema.json` is a JSON Schema (draft 2020-12) for `config.json`. `auth.json` is not covered.

Regenerate it after changing any struct in `internal/config`:

```sh
go generate ./internal/config
```

`go run ./cmd/schemagen` from the repository root does the same. Output is deterministic, so regenerating an unchanged tree reproduces the file byte for byte.

How the generator maps Go to the schema (`github.com/invopop/jsonschema`, MIT):

- Property names and nesting follow the `json` struct tags. Fields tagged `json:"-"` (the in-memory `Auth`) are left out.
- Descriptions come from Go comments: a field's doc comment and its trailing line comment. One-line section headers such as `// Manager settings` are skipped. Type doc comments describe the `$defs` entries.
- A property whose comment has a `Deprecated:` or `Deprecated.` line gets `"deprecated": true`.
- Credentials get `"writeOnly": true`, and string credentials also get `"format": "password"`. The list is `secretProperties` in `cmd/schemagen/main.go`; generation fails if a listed property no longer exists.
- No property is `required` and `additionalProperties` is left open, because the Go loader accepts a partial file and ignores unknown keys.
- The schema has no `default` values yet. Defaults live in `setDefaults` and `updateDebrid` in `internal/config`.

## Later: TS and Rust types

TypeScript types will be generated from `config.schema.json` with `json-schema-to-typescript`, and Rust types with `typify` (Apache-2.0). Neither is generated yet; the schema is the single source both will be built from.
