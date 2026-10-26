# Commands

Run `envhaul <command> --help` for every flag, or see the
[CLI reference](reference/cli/envhaul.md).

| Command | Contacts AWS | Writes files | Purpose |
| --- | --- | --- | --- |
| `sync [component...]` | yes | yes | Resolve values and write env files |
| `diff [component...]` | yes | no | Show what `sync` would change; values masked |
| `get <component> <VAR>` | yes | no | Print one value |
| `exec <component> -- cmd` | yes | no | Run a command with the values in its environment |
| `list [component...]` | no | no | Show each variable's source |
| `validate` | no | no | Check the config |
| `init` | no | yes | Create a starter config |
| `version` | no | no | Print build information |
| `completion <shell>` | no | no | Print a shell completion script |

Global flags: `-c/--config`, `--profile`, `--region`, `-v/--verbose` (logs
each request, never values) and `--no-color` (envhaul also honors `NO_COLOR`).

## sync

```sh
envhaul sync                 # every component
envhaul sync api web         # just these
envhaul sync --strict        # fail if any referenced value is missing
```

Nothing is written unless every selected component resolves.

## diff

```sh
envhaul diff                 # + added, ~ changed, - removed; values masked
envhaul diff --show-values   # include values
envhaul diff --exit-code     # exit 1 when anything would change
```

## get

```sh
envhaul get api DATABASE_URL
export TOKEN="$(envhaul get ci GITHUB_TOKEN)"
envhaul get api KEY -n | pbcopy   # no trailing newline
```

Only the references that variable uses are fetched. A missing value is an
error, unless the reference has a `|default`.

## exec

```sh
envhaul exec api -- npm run dev
envhaul exec infrastructure -- cdk deploy
```

The component's variables are added to the current environment, replacing
any that are already set. Nothing is written to disk. On macOS and Linux the
command replaces the envhaul process, so signals and exit codes behave as if
you ran it directly.

## list

```sh
envhaul list
envhaul list api --json | jq -r '.[] | select(.kind != "inline") | .var'
```

The `--json` output is stable: each entry has `component`, `env_file`, `var`,
`kind` (`inline`, `reference` or `template`) and `sources` (each with
`scheme`, `path`, `key`, `profile`, `region` and `pipes`).

## validate

```sh
envhaul validate
```

Reports every problem, not just the first, with `file:line:column`:

```
envhaul.yaml:12:7: invalid variable name "API-KEY" (must match [A-Za-z_][A-Za-z0-9_]*)
envhaul.yaml:18:21: unknown pipe function "urlencoded" (available: base64, base64decode, default, lower, trim, upper, urlencode)
invalid: 2 problems
```

## init

```sh
envhaul init
envhaul init --project shop --profile shop-dev --region eu-west-1 --yes
```

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Error, invalid config, missing values with `--strict`, or changes found by `diff --exit-code` |
| other | `exec` passes through the command's exit code |
