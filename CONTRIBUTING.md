# Contributing to envhaul

Thanks for helping! Bug reports, docs fixes, new sources and features are all
welcome. Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first.

## Before you start

- **Bugs:** open an issue using the bug report form, including the output of
  `envhaul version` and a minimal config that reproduces the problem (no real
  secrets).
- **Features and new sources:** open an issue to discuss the design before
  writing a large PR. New value sources have their own issue form.
- **Security issues:** follow [SECURITY.md](SECURITY.md); do not open a
  public issue.

## Development setup

You need Go (the version in `go.mod` or newer) and, for integration tests,
Docker.

```sh
git clone https://github.com/davidtucker/envhaul
cd envhaul
make test        # unit and CLI tests with the race detector
make lint        # golangci-lint, same version as CI
make build       # ./bin/envhaul
```

Run `make help` for every target. Useful ones:

| Target | What it does |
| --- | --- |
| `make test` | Unit tests plus the CLI scripts in `internal/cli/testdata/script` |
| `make fuzz` | Fuzzes the template parser and dotenv round trip |
| `make emulator-up` / `make test-integration` | Runs envhaul against moto, an AWS emulator |
| `make snapshot` | Builds every release artifact locally |
| `make docs` | Serves the documentation site at http://127.0.0.1:8000 |

## Project layout

```
cmd/envhaul/          main package
internal/cli/         cobra commands; testdata/script holds end-to-end CLI tests
internal/config/      config loading and validation (file:line:col diagnostics)
internal/template/    {{scheme:path#key@profile|pipe}} parser
internal/pipe/        pipe functions (urlencode, default, ...)
internal/provider/    Provider interface and the AWS implementations
internal/providers/   wiring of built-in providers
internal/resolve/     dedupe, fetch and render values
internal/dotenv/      .env rendering, parsing and atomic writes
schema/               JSON Schema for editor support
docs/                 documentation site (MkDocs Material)
```

## Tests

- Every change needs tests. Most behavior is easiest to cover with a
  [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)
  file in `internal/cli/testdata/script`. Those scripts run the real CLI and
  real providers against fake AWS clients that read `fake-aws.json`.
- Provider code should also have unit tests against fake SDK clients, and an
  integration test in `test/integration` when the emulator supports it.
- Never log or print secret values, including in `--verbose` output and error
  messages. The integration test checks this.

## Adding a value source

A source is a package under `internal/provider/` implementing
`provider.Provider`:

1. `Scheme()` returns the reference prefix, e.g. `vault`.
2. `Validate(ref)` performs offline checks (used by `validate`).
3. `Request(ref)` applies prefixes and default profile settings.
4. `Fetch(ctx, reqs)` fetches values, batching where the API allows. A value
   that doesn't exist is `Found: false`, not an error.

Then register it in `internal/providers/providers.go`, document it under
`docs/sources/`, and add tests. See the
[provider guide](https://davidtucker.github.io/envhaul/contributing/providers/)
for details.

## Commits and pull requests

- PRs are **squash-merged**, and the PR title becomes the commit message.
  Titles must follow [Conventional Commits](https://www.conventionalcommits.org/),
  because [release-please](https://github.com/googleapis/release-please)
  derives versions and the changelog from them:
  - `feat: add vault source` → minor release
  - `fix: quote values containing #` → patch release
  - `feat!: rename env_file to output` (or a `BREAKING CHANGE:` footer) → major release
  - `docs:`, `test:`, `refactor:`, `ci:`, `chore:` → no release on their own
- Keep PRs focused, and update the docs and `schema/envhaul.schema.json`
  when behavior or config changes.
- CI must pass: tests on Linux, macOS and Windows, lint, fuzzing, integration
  tests and a release dry run.

## Releases

Maintainers merge the release PR that release-please keeps open. That tags
the version, and GoReleaser publishes binaries, packages, the container image,
the Homebrew cask and the Scoop manifest. See
[docs/maintainers.md](docs/maintainers.md).

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE), the same license as the project. No CLA is
required.
