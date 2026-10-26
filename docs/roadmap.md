# Roadmap

envhaul reads from AWS SSM Parameter Store and Secrets Manager, HashiCorp
Vault and OpenBao, 1Password, Google Cloud Secret Manager and Azure Key Vault
(see [Sources](sources/index.md)).

More sources will follow, using the same rules:

- **Your existing sign-in.** A source uses the credentials its own tools
  already set up; envhaul doesn't add a login flow.
- **The same reference syntax.** `#key`, versions, `@profile` and pipes work
  as they do for every other source.
- **Offline checks.** `validate` and `list` keep working without network
  access.

This page lists what's planned, not what's promised. New sources are
announced here before they're built.

## Have a say

Want a source that isn't supported yet?
[Propose a source](https://github.com/davidtucker/envhaul/issues/new?template=new_provider.yml)
or add a 👍 to an existing proposal. To build one yourself, see
[Adding a source](contributing/providers.md).
