<!--
The PR title becomes the commit message and drives the changelog. Use
Conventional Commits, e.g. "feat: add vault source" or "fix: quote values with #".
-->

## What and why

<!-- What does this change, and why? Link the issue it closes: "Closes #123". -->

## How it was tested

<!-- Tests added or updated, and anything you checked by hand. -->

## Checklist

- [ ] Tests cover the change (unit tests and/or a script in `internal/cli/testdata/script`)
- [ ] Docs updated (`docs/`, `README.md`, and `schema/envhaul.schema.json` if the config changed)
- [ ] No secret values can reach logs, errors or `diff` output
- [ ] Breaking changes are marked with `!` in the title and explained above
