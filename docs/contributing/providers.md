# Adding a source

Sources live under `internal/provider/` and implement one interface:

```go
type Provider interface {
    Scheme() string                     // "example"
    Description() string                // "Example Secrets Service"
    Validate(ref *template.Ref) error   // offline checks, run by `validate`
    Request(ref *template.Ref) Request  // apply prefixes and defaults
    Fetch(ctx context.Context, reqs []Request) (map[Request]Result, error)
}
```

## What envhaul does for you

The resolver parses templates, collects every distinct `Request` for your
scheme across all components, and calls `Fetch` once with all of them. It then
handles `#key` JSON extraction, pipes, missing-value reporting and
`--strict`. Your provider only maps requests to values.

The built-in providers are good models: `internal/provider/vault` (plain HTTP
API), `internal/provider/onepassword` (wrapping a CLI) and
`internal/provider/gcpsecrets` (REST with OAuth credentials).

## Guidelines

- **Batch.** Fetch as many values per API call as the service allows, and
  bound concurrency (see `awsssm` for an example).
- **Missing isn't an error.** Return `Result{Found: false}` for values that
  don't exist. Return errors only for failures such as authentication,
  authorization or network problems, and say how to fix them.
- **`@profile`** is the user's way to pick credentials or an account. Map it
  to whatever the source uses (a Vault namespace, a 1Password account) and
  document it.
- **Never log values.** Log identifiers at debug level only.
- **Offline first.** `Validate` and `Request` must work without network
  access; `validate` and `list` rely on that.

## Checklist

1. A package `internal/provider/<name>` with the provider and unit tests
   against a fake client interface.
2. Registration in `internal/providers/providers.go`, and, if the source has
   settings, its block in `config.SourceKeys` and `schema/envhaul.schema.json`.
3. A testscript in `internal/cli/testdata/script` if the CLI behavior
   changes.
4. An integration test in `test/integration` if an emulator exists.
5. Docs: a page under `docs/sources/`, plus the table in
   `docs/sources/index.md`.
