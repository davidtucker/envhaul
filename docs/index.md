---
hide:
  - navigation
---

<div class="envhaul-hero" markdown>
![envhaul](assets/brand/logo-light.svg){ .logo-light }
![envhaul](assets/brand/logo-dark.svg){ .logo-dark }

<p class="envhaul-tagline">Haul config and secrets from your vaults into <code>.env</code> files.</p>
</div>

**envhaul** (pronounced *E-N-V-haul*) generates the `.env` files for every
part of your project from one config file. Plain values sit in the config next to references to your
secret stores: AWS SSM Parameter Store and Secrets Manager, HashiCorp Vault
and OpenBao, 1Password, Google Cloud Secret Manager and Azure Key Vault.
envhaul resolves them with the sign-ins you already have, such as your named
AWS profiles.

```yaml title="envhaul.yaml"
name: dev
aws_profile: acme-dev
aws_region: us-east-1
ssm_prefix: /acme/dev

components:
  api:
    env_file: services/api/.env
    vars:
      APP_ENV: development
      STRIPE_SECRET_KEY: "{{ssm:api/STRIPE_SECRET_KEY}}"
      OPENAI_API_KEY: "{{ssm:api/OPENAI_API_KEY@acme-shared}}"
      DATABASE_URL: "postgresql://{{secret:acme/dev/db#username}}:{{secret:acme/dev/db#password|urlencode}}@localhost/acme"
  web:
    env_file: web/.env
    vars:
      VITE_API_BASE_URL: "http://localhost:8000"
```

```console
$ envhaul sync
wrote  services/api/.env  (4 vars)
wrote  web/.env           (1 var)
```

## Why envhaul

- **One file, every component.** API, frontend, infrastructure and tools each
  get their own `.env`, generated from one place you can review.
- **Secrets stay where they are.** Only references are committed. Values
  are fetched on demand, written with mode `0600`, and never logged.
- **Many sources, one file.** Mix AWS, Vault, 1Password, Google Cloud and
  Azure in the same config, even in the same value.
- **Your existing sign-ins.** AWS profiles (SSO, assume-role,
  `credential_process`), `vault login`, the `op` CLI, `gcloud` and
  `az login` all work as they are, and `@profile` reaches other accounts,
  namespaces, projects and tenants.
- **Templates and pipes.** Build connection strings from several secrets and
  `urlencode`, `trim`, `base64` or `default` them inline.
- **Safe by default.** `diff` masks values, `validate` catches mistakes
  offline with `file:line:col` errors, and `exec` runs a command without
  writing secrets to disk.
- **Fast.** Identical references are fetched once, SSM reads are batched, and
  requests run concurrently.

[Get started](getting-started.md){ .md-button .md-button--primary }
[Configuration reference](config.md){ .md-button }
