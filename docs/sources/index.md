# Sources

A source is where a reference's value comes from, selected by the scheme
before the first `:`.

| Scheme | Source | Since |
| --- | --- | --- |
| `ssm` | [AWS SSM Parameter Store](ssm.md) | v0.1 |
| `secret` | [AWS Secrets Manager](secrets-manager.md) | v0.1 |

More sources are planned after v1. Have one in mind?
[Propose it](https://github.com/davidtucker/envhaul/issues/new?template=new_provider.yml),
or read [Adding a source](../contributing/providers.md).
