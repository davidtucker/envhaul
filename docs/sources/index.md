# Sources

A source is where a reference's value comes from, selected by the scheme
before the first `:`.

| Scheme | Source | Signs in with | `@profile` picks |
| --- | --- | --- | --- |
| `ssm` | [AWS SSM Parameter Store](ssm.md) | Your AWS profiles | An AWS profile |
| `secret` | [AWS Secrets Manager](secrets-manager.md) | Your AWS profiles | An AWS profile |
| `vault` | [HashiCorp Vault and OpenBao](vault.md) | `vault login` / `VAULT_TOKEN` | A profile, or a namespace |
| `op` | [1Password](1password.md) | The `op` CLI | A profile, or an account |
| `gcp` | [Google Cloud Secret Manager](gcp.md) | `gcloud auth application-default login` | A profile, or a project |
| `azkv` | [Azure Key Vault](azure-key-vault.md) | `az login` | A profile, or a tenant |

Every source reuses the sign-in its own tools already set up; envhaul has no
login of its own. Sources other than AWS take their defaults and named
profiles from a block in `envhaul.yaml` (see
[Source settings](../config.md#source-settings)), and all of them work with
`#key`, pipes, `validate` and `list` the same way.

Have another source in mind?
[Propose it](https://github.com/davidtucker/envhaul/issues/new?template=new_provider.yml),
or read [Adding a source](../contributing/providers.md).
