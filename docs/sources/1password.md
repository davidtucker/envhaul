# 1Password

```
{{op:vault/item/field[@profile]}}
{{op:vault/item/section/field[@profile]}}
```

Reads fields from [1Password](https://1password.com) through the
[1Password CLI](https://developer.1password.com/docs/cli/) (`op`), using the
same `vault/item/field` form as 1Password's own `op://` secret references.

```yaml
onepassword:
  account: my-team.1password.com   # optional

components:
  api:
    env_file: .env
    vars:
      STRIPE_KEY: "{{op:Engineering/Stripe/credential}}"
      DB_PASSWORD: "{{op:op://Engineering/Postgres/password}}"  # op:// prefix is fine
      DEPLOY_KEY: "{{op:Ops/Deploy key/private key}}"
```

- **Reference:** `vault/item/field`, or `vault/item/section/field`. Copy it
  from the 1Password app with **Copy Secret Reference**, then drop the
  `op://` (or keep it).
- **Spaces** in vault, item or field names are fine, as in the `Deploy key`
  example.

## Signing in

envhaul runs `op read` for each reference, so you need the `op` CLI
installed and signed in the usual way:

- **On your machine:** turn on **Integrate with 1Password CLI** in the
  1Password app's Developer settings. The app asks you to approve access;
  envhaul reads the first reference on its own before the rest, so you
  approve once rather than once per secret.
- **In CI:** set `OP_SERVICE_ACCOUNT_TOKEN` to a
  [service account](https://developer.1password.com/docs/service-accounts/)
  token.

## Accounts and profiles

If you belong to several 1Password accounts, `account:` picks the default
and `@name` picks another for one reference. `@name` is a profile from the
`onepassword:` block, or, if there's no such profile, an account
(a sign-in address like `my-team.1password.com`, an email, or an account ID):

```yaml
onepassword:
  account: my-team.1password.com
  profiles:
    personal:
      account: my.1password.com
```

```yaml
PERSONAL_TOKEN: "{{op:Private/GitHub/token@personal}}"
OTHER_ACCOUNT:  "{{op:Shared/API/key@other-team.1password.com}}"
```

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `account` | `op`'s default | Account passed to `op --account` |

## Missing values and errors

An item, vault or field that doesn't exist counts as missing (see
[Missing values](../config.md#missing-values)). If `op` isn't installed or
isn't signed in, envhaul stops and says how to fix it. Reads run up to 4 at a
time.
