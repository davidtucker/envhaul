# Using envhaul in CI

## Check the config on every pull request

`validate` and `list` never contact AWS, so they need no credentials:

```yaml title=".github/workflows/envhaul.yml"
name: envhaul
on: pull_request
permissions:
  contents: read
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - run: docker run --rm -v "$PWD:/work" -w /work ghcr.io/davidtucker/envhaul validate
```

## Generate env files in a pipeline

Use OIDC to assume a role, then run `sync --strict` so missing values fail the
build:

```yaml
permissions:
  id-token: write
  contents: read
steps:
  - uses: actions/checkout@v7
  - uses: aws-actions/configure-aws-credentials@v5
    with:
      role-to-assume: arn:aws:iam::111111111111:role/envhaul-ci
      aws-region: us-east-1
  - run: curl -fsSL https://raw.githubusercontent.com/davidtucker/envhaul/main/install.sh | sh
  - run: envhaul sync --strict --profile ""
```

`--profile ""` ignores `aws_profile` from the config so the SDK uses the
credentials from the environment. References with an explicit `@profile`
still need that profile to exist on the runner.

## Detect drift

```sh
envhaul diff --exit-code
```

Exits 1 when a generated file is out of date. This is useful in a pre-commit
hook or a scheduled job.
