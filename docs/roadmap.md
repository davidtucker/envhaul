# Roadmap

envhaul reads from AWS SSM Parameter Store and Secrets Manager today. Next we
plan to add sources beyond AWS, so one config can pull from wherever your team
keeps its secrets.

This page lists what's planned, not what's promised. Plans can change, and
there are no dates yet.

## Planned sources

| Source | What it would read |
| --- | --- |
| HashiCorp Vault and OpenBao | Secrets in the KV secrets engine, including a single field or a specific version |
| 1Password | Fields of items in your 1Password vaults |
| Google Cloud Secret Manager | Secrets and secret versions in your Google Cloud projects |
| Azure Key Vault | Secrets in your Azure key vaults |

Each source will follow the same rules as the AWS ones:

- **Your existing sign-in.** envhaul will use the credentials the source's own
  tools already set up (`vault login`, the `op` CLI, `gcloud`, `az login`)
  and won't add a login flow of its own.
- **The same reference syntax.** `#key`, versions, `@profile` and pipes will
  work as they do for SSM and Secrets Manager, with `@profile` choosing the
  account or credentials the source uses.
- **Offline checks.** `validate` and `list` will keep working without network
  access.

## Have a say

Want one of these sooner, or a source that isn't listed?
[Propose a source](https://github.com/davidtucker/envhaul/issues/new?template=new_provider.yml)
or add a 👍 to an existing proposal. To build one yourself, see
[Adding a source](contributing/providers.md).
