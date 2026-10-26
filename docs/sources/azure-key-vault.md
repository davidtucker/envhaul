# Azure Key Vault

```
{{azkv:vault-name/secret-name[:version][#key][@profile]}}
{{azkv:secret-name[:version][#key][@profile]}}
```

Reads secrets from [Azure Key Vault](https://learn.microsoft.com/azure/key-vault/).

```yaml
azure:
  vault: dev-kv            # default vault for names without a vault/

components:
  api:
    env_file: .env
    vars:
      DB_PASSWORD: "{{azkv:db-password}}"           # dev-kv
      SHARED_KEY: "{{azkv:shared-kv/api-key}}"      # another vault
      PINNED: "{{azkv:db-password:0123456789abcdef0123456789abcdef}}"
      DB_USER: "{{azkv:db-credentials#username}}"   # key of a JSON secret
```

- **Names:** `vault-name/secret-name`, or just `secret-name` with a default
  `vault:`. Key Vault names allow only letters, digits and hyphens, so
  `envhaul validate` catches typos (like an underscore) before any network
  call.
- **Versions:** `:version-id` reads a specific version; the current version
  otherwise.

## Signing in

envhaul uses Azure's default credential chain, the same one the Azure SDKs
use. It tries, in order:

1. a service principal from environment variables
   (`AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`, …);
2. workload identity (AKS, GitHub Actions federation);
3. managed identity (Azure VMs, App Service and similar);
4. your `az login` session (Azure CLI), then `azd auth login`, then Azure
   PowerShell's `Connect-AzAccount`.

On your machine that usually means just running `az login`.

## Tenants and profiles

`@name` selects a profile from the `azure:` block, or, if there's no such
profile, is used as the **tenant ID** to authenticate against:

```yaml
azure:
  vault: dev-kv
  profiles:
    prod:
      vault: prod-kv
      tenant: 11111111-2222-3333-4444-555555555555
    gov:
      vault: gov-kv
      vault_dns_suffix: vault.usgovcloudapi.net
```

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `vault` | | Default vault for names without a vault prefix |
| `tenant` | Your default tenant | Microsoft Entra tenant ID |
| `vault_dns_suffix` | `vault.azure.net` | DNS suffix for sovereign clouds |

## API usage and permissions

envhaul calls Get Secret for each secret, running up to 8 at once. The
identity needs the **Key Vault Secrets User** role (Azure RBAC) or a get-secret
access policy. A secret that doesn't exist counts as missing; a permission
error, or a disabled secret, stops the run.
