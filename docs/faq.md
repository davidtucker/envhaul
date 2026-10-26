# FAQ

## How is this different from chamber, aws-vault, sops or dotenv-vault?

- **chamber** stores secrets under per-service paths and execs or exports
  them. envhaul instead maps every variable of every component in one
  reviewable config: inline values next to references, templates that combine
  several values, and references to SSM and Secrets Manager in several
  accounts in the same file.
- **aws-vault** manages AWS credentials. It pairs well with envhaul: envhaul
  uses whatever credentials your profiles provide.
- **sops** encrypts secrets inside files in your repository. envhaul keeps
  secrets in AWS and commits only references.
- **dotenv-vault** syncs encrypted `.env` files through a hosted service.
  envhaul has no service; it talks to your AWS accounts directly.

## Can I keep local-only values?

envhaul overwrites the files it generates. Put local values in the config as
inline values, or in a second file your tools load after the generated one
(Vite, Next.js and python-dotenv all support `.env.local`-style overrides).

## Why is a missing parameter a warning and not an error?

Teams often reference optional integrations that not everyone has set up yet.
A warning keeps everyone else working. Use `--strict` in CI, or `|default:` to
make the fallback explicit.

## Does it work with SSO / Identity Center?

Yes, along with assume-role, `credential_process` and anything else the AWS
SDK supports. When a session has expired, envhaul tells you which
`aws sso login` command to run.

## Does it support other secret stores?

v1 supports AWS SSM Parameter Store and Secrets Manager. The source layer is
pluggable and more sources are planned; see [Adding a source](contributing/providers.md).

## Why does `1.10` stay `1.10`?

envhaul reads YAML scalars exactly as written rather than converting them, so
versions, octal-looking numbers and words like `yes` and `no` come through
unchanged.
