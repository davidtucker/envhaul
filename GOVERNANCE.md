# Governance

envhaul is maintained by the people listed in [MAINTAINERS.md](MAINTAINERS.md).

## Decisions

- Day-to-day decisions (reviews, merges, triage) are made by any maintainer.
- Larger changes, such as new sources, config schema changes, breaking
  changes or new dependencies, start as an issue or discussion. They need
  approval from at least one maintainer who didn't write the change, after
  giving the community a reasonable chance (normally a week) to comment.
- If maintainers disagree, the lead maintainer decides.

## Compatibility promise

From v1.0.0 onwards:

- config files with `version: 1` stay valid for every v1.x release,
- commands, flags and `--json` output follow Semantic Versioning,
- anything deprecated prints a warning for at least one minor release before
  it is removed in a major release.

## Becoming a maintainer

Contributors who have made sustained, high-quality contributions (code,
reviews, docs, triage) may be invited by the existing maintainers. Maintainers
who are inactive for 12 months may be moved to emeritus status.

## Code of Conduct

Everyone participating in the project follows the
[Code of Conduct](CODE_OF_CONDUCT.md).
