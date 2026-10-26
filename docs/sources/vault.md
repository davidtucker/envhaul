# HashiCorp Vault and OpenBao

```
{{vault:path[:version][#key][@profile]}}
```

Reads from the KV secrets engine of [HashiCorp Vault](https://developer.hashicorp.com/vault)
or [OpenBao](https://openbao.org), whose API is compatible.

```yaml
vault:
  address: https://vault.example.com   # default: $VAULT_ADDR
  mount: secret                        # default: secret

components:
  api:
    env_file: .env
    vars:
      DB_USER: "{{vault:app/db#user}}"            # secret/app/db, field "user"
      DB_PASS: "{{vault:app/db#password|urlencode}}"
      PINNED: "{{vault:app/db:3#password}}"       # version 3
      OTHER_MOUNT: "{{vault:/kv/team/api#key}}"   # mount "kv"
```

- **Path:** without a leading `/`, the path is under the configured `mount`
  (`secret` by default). With a leading `/`, the first segment is the mount,
  as in `/kv/team/api`. For a nested mount such as `team/kv`, set `mount:`.
- **Fields:** a Vault secret holds several fields. `#key` picks one; without
  it you get the whole secret as a JSON object.
- **Versions:** `:3` reads version 3 (KV v2). Without a version you get the
  latest.
- **KV v1:** set `kv_version: 1` for mounts using the version 1 engine.

## Signing in

envhaul uses the same token the `vault` CLI does, in this order:

1. the environment variable named by `token_env`, if set;
2. `VAULT_TOKEN`;
3. `~/.vault-token`, which `vault login` writes.

The address comes from `address` or `VAULT_ADDR`. For a server with a
private CA, set `VAULT_CACERT` to the CA certificate file.

## Profiles and namespaces

`@name` selects a profile from the `vault:` block, whose settings override
the defaults. Use this for a second Vault server or different credentials:

```yaml
vault:
  address: https://vault.example.com
  profiles:
    prod:
      address: https://vault.prod.example.com
      token_env: VAULT_PROD_TOKEN
    team:
      namespace: team-a
```

If `@name` doesn't match a profile, it's used as the **namespace** (Vault
Enterprise and OpenBao), so `{{vault:app/db#key@team-a}}` works without any
profile. `namespace:` and `VAULT_NAMESPACE` set a default namespace.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `address` | `$VAULT_ADDR` | Server address |
| `namespace` | `$VAULT_NAMESPACE` | Namespace (Enterprise, OpenBao) |
| `mount` | `secret` | KV mount for paths without a leading `/` |
| `kv_version` | `2` | KV engine version, `1` or `2` |
| `token_env` | | Environment variable holding the token |

## API usage and permissions

envhaul reads with `GET /v1/<mount>/data/<path>` (KV v2) or
`GET /v1/<mount>/<path>` (KV v1), running up to 8 reads at once; Vault has
no batch read. A policy that allows envhaul to read `app/*` in the `secret`
mount:

```hcl
path "secret/data/app/*" {
  capabilities = ["read"]
}
```

A secret that doesn't exist, or a deleted version, counts as missing (see
[Missing values](../config.md#missing-values)). A permission error stops the
run and suggests `vault login`.
