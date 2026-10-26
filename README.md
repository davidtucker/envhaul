<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/assets/logo-dark.svg">
    <img alt="envhaul" src=".github/assets/logo-light.svg" width="420">
  </picture>
</p>
<p align="center">Haul config and secrets from your vaults into <code>.env</code> files.</p>

<p align="center">
  <a href="https://github.com/davidtucker/envhaul/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/davidtucker/envhaul/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/davidtucker/envhaul/releases"><img alt="Release" src="https://img.shields.io/github/v/release/davidtucker/envhaul?sort=semver"></a>
  <a href="https://pkg.go.dev/github.com/davidtucker/envhaul"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/davidtucker/envhaul.svg"></a>
  <a href="https://codecov.io/gh/davidtucker/envhaul"><img alt="Coverage" src="https://codecov.io/gh/davidtucker/envhaul/graph/badge.svg"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/davidtucker/envhaul"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/davidtucker/envhaul/badge"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
</p>

---

**envhaul** (pronounced *E-N-V-haul*) generates the `.env` files for every
part of your project from one reviewable config file. Plain values live in the config next to references
to your secret stores (**AWS SSM Parameter Store and Secrets Manager,
HashiCorp Vault and OpenBao, 1Password, Google Cloud Secret Manager and
Azure Key Vault**), which envhaul resolves with the sign-ins you already
have, such as your named AWS profiles across as many accounts as you need.

```yaml
# envhaul.yaml
name: dev
aws_profile: acme-dev
aws_region: us-east-1
ssm_prefix: /acme/dev

components:
  api:
    env_file: services/api/.env
    vars:
      APP_ENV: development                                       # inline, committed
      STRIPE_SECRET_KEY: "{{ssm:api/STRIPE_SECRET_KEY}}"         # /acme/dev/api/STRIPE_SECRET_KEY
      OPENAI_API_KEY: "{{ssm:api/OPENAI_API_KEY@acme-shared}}"   # another account
      DATABASE_URL: "postgresql://{{secret:acme/dev/db#username}}:{{secret:acme/dev/db#password|urlencode}}@localhost/acme"
      SIGNING_KEY: "{{vault:payments/keys#signing}}"            # HashiCorp Vault
      GITHUB_TOKEN: "{{op:Engineering/GitHub/token}}"           # 1Password
  web:
    env_file: web/.env
    vars:
      VITE_API_BASE_URL: "http://localhost:8000"
      VITE_SENTRY_DSN: "{{ssm:api/SENTRY_DSN@acme-shared|default:}}"
```

```console
$ envhaul sync
wrote  services/api/.env  (6 vars)
wrote  web/.env           (2 vars)
```

## Features

- **One config, every component.** API, frontend, infrastructure and tooling
  each get their own `.env`, in variable order, from one file you can review.
- **Secrets stay where they are.** Only references are committed. Generated
  files are written atomically with mode `0600`, and values never appear in
  logs.
- **Many sources, one file.** AWS, Vault/OpenBao, 1Password, Google Cloud and
  Azure side by side, even within one value.
- **Your existing sign-ins.** AWS profiles (SSO, assume-role,
  `credential_process`), `vault login`, the `op` CLI, `gcloud` and `az login`
  work as they are. `@profile` reaches other accounts, namespaces, projects
  and tenants.
- **Templates and pipes.** Build connection strings from several secrets, and
  apply `urlencode`, `trim`, `base64`, `default:` and more.
- **Safe workflows.** `validate` checks the config offline with
  `file:line:col` errors, `diff` previews changes with values masked, and
  `exec` runs a command without writing secrets to disk.
- **Fast.** Each value is fetched once per run, SSM reads are batched, and
  requests run concurrently.

## Install

### macOS and Linux: Homebrew

```sh
brew install davidtucker/tap/envhaul
```

### Windows: Scoop

```powershell
scoop bucket add davidtucker https://github.com/davidtucker/scoop-bucket
scoop install envhaul
```

### Linux: deb, rpm or apk package

Download the package for your distribution and architecture from the
[latest release](https://github.com/davidtucker/envhaul/releases/latest), then:

```sh
sudo dpkg -i envhaul_*_amd64.deb                     # Debian, Ubuntu
sudo rpm -i envhaul-*.x86_64.rpm                     # Fedora, RHEL
sudo apk add --allow-untrusted envhaul_*_x86_64.apk  # Alpine
```

### macOS and Linux: install script

```sh
curl -fsSL https://raw.githubusercontent.com/davidtucker/envhaul/main/install.sh | sh
```

Installs the latest release to `/usr/local/bin` (or `~/.local/bin`) after
verifying its checksum, and its signature too if
[cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is
installed.

### Any platform: Go

```sh
go install github.com/davidtucker/envhaul/cmd/envhaul@latest
```

Requires Go 1.26 or newer.

### Docker

```sh
docker run --rm -v "$PWD:/work" -w /work \
  -v "$HOME/.aws:/home/nonroot/.aws:ro" \
  ghcr.io/davidtucker/envhaul sync
```

### Manual download

Archives for every platform are on the
[releases page](https://github.com/davidtucker/envhaul/releases). Every
release is signed and has build provenance; see
[verifying releases](https://davidtucker.github.io/envhaul/security/#verifying-releases).

## Quick start

```sh
envhaul init        # write a starter envhaul.yaml and git-ignore .env
envhaul validate    # check it offline
envhaul list        # show where every value comes from
envhaul diff        # preview changes (values masked)
envhaul sync        # write the .env files
```

| Command | What it does |
| --- | --- |
| `sync [component...]` | Resolve values and write env files (`--strict` fails on missing values) |
| `diff [component...]` | Show what would change; `--show-values`, `--exit-code` |
| `get <component> <VAR>` | Print one resolved value |
| `exec <component> -- cmd` | Run a command with the values in its environment, without writing a file |
| `list [component...]` | Show each variable's source (offline, `--json`) |
| `validate` | Check the config (offline) |
| `init` | Create a starter config |

## Sources

| Scheme | Source | Example |
| --- | --- | --- |
| `ssm` | AWS SSM Parameter Store | `{{ssm:api/KEY}}` |
| `secret` | AWS Secrets Manager | `{{secret:acme/dev/db#password}}` |
| `vault` | HashiCorp Vault and OpenBao (KV v1 and v2) | `{{vault:app/db#password}}` |
| `op` | 1Password (through the `op` CLI) | `{{op:Engineering/Stripe/credential}}` |
| `gcp` | Google Cloud Secret Manager | `{{gcp:db-password:3}}` |
| `azkv` | Azure Key Vault | `{{azkv:dev-kv/db-password}}` |

Each source page in the [documentation](https://davidtucker.github.io/envhaul/sources/)
covers sign-in, profiles, settings and required permissions.

## Reference syntax

```
{{ scheme:path [#key] [@profile] [| fn[:arg]]... }}
```

| Example | Reads |
| --- | --- |
| `{{ssm:api/KEY}}` | SSM parameter `api/KEY`, joined onto `ssm_prefix` |
| `{{ssm:/abs/path:3}}` | SSM parameter at an absolute path, version 3 |
| `{{secret:db}}` | The whole Secrets Manager secret |
| `{{secret:db#password}}` | One key of a JSON secret |
| `{{ssm:api/KEY@shared}}` | Using the `shared` AWS profile |
| `{{vault:app/db:3#password@prod}}` | Version 3 of a Vault secret, one field, the `prod` profile |
| `{{secret:db#pass\|urlencode}}` | URL-encoded, e.g. for connection strings |
| `{{ssm:opt\|default:fallback}}` | `fallback` when missing or empty |

Pipes: `urlencode`, `base64`, `base64decode`, `trim`, `lower`, `upper`,
`default:value`. See the [reference guide](https://davidtucker.github.io/envhaul/references/).

## Documentation

Full documentation is at **[davidtucker.github.io/envhaul](https://davidtucker.github.io/envhaul)**:

- [Configuration](https://davidtucker.github.io/envhaul/config/)
- [AWS profiles and permissions](https://davidtucker.github.io/envhaul/aws/), including a least-privilege IAM policy
- [Using envhaul in CI](https://davidtucker.github.io/envhaul/ci/)
- [Security model and verifying releases](https://davidtucker.github.io/envhaul/security/)

## How it fits with other tools

envhaul focuses on one job: a reviewable, per-component map from variable
names to values in AWS, resolved across accounts with your own profiles and
written as `.env` files. It works alongside credential managers such as
aws-vault, and differs from tools that encrypt secrets in the repository
(sops) or sync them through a hosted service. See the
[FAQ](https://davidtucker.github.io/envhaul/faq/) for details.

## Versioning

envhaul follows [Semantic Versioning](https://semver.org/). From v1.0.0,
config files with `version: 1`, command-line flags and `--json` output stay
compatible across every v1.x release; deprecations warn for at least one
minor release first. See the [changelog](CHANGELOG.md).

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) and
the [Code of Conduct](CODE_OF_CONDUCT.md). Want another secret store? Open a
[new source proposal](https://github.com/davidtucker/envhaul/issues/new?template=new_provider.yml).

Report security issues privately; see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
