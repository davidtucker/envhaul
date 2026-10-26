# Google Cloud Secret Manager

```
{{gcp:name[:version][#key][@profile]}}
{{gcp:projects/PROJECT/secrets/NAME[/versions/VERSION][#key]}}
```

Reads secret versions from
[Google Cloud Secret Manager](https://cloud.google.com/secret-manager).

```yaml
gcp:
  project: my-dev-project

components:
  api:
    env_file: .env
    vars:
      DB_PASSWORD: "{{gcp:db-password}}"            # latest version
      PINNED: "{{gcp:db-password:3}}"               # version 3
      DB_USER: "{{gcp:db-credentials#username}}"    # key of a JSON secret
      SHARED: "{{gcp:projects/shared-prj/secrets/api-key}}"
```

- **Names:** a short name is looked up in the configured project. A full
  resource name (`projects/.../secrets/...`) names its own project.
- **Versions:** `:3` for version 3, or a version alias; `latest` by
  default.
- **JSON secrets:** `#key` selects a top-level key.

## Signing in

envhaul uses [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials):

- **On your machine:** `gcloud auth application-default login`.
- **In CI:** workload identity federation or a service account key in
  `GOOGLE_APPLICATION_CREDENTIALS`.
- **On Google Cloud:** the attached service account, automatically.

### Which project

For short names, the project comes from, in order: the profile or `project:`
setting, `GOOGLE_CLOUD_PROJECT`, `CLOUDSDK_CORE_PROJECT`, then the
credentials' own project.

## Profiles and impersonation

`@name` selects a profile from the `gcp:` block, or, if there's no such
profile, is used as the **project ID**:

```yaml
gcp:
  project: my-dev-project
  profiles:
    prod:
      project: my-prod-project
      impersonate_service_account: secrets-reader@my-prod-project.iam.gserviceaccount.com
```

```yaml
PROD_DB: "{{gcp:db-password@prod}}"            # profile: impersonates the reader
STAGING_DB: "{{gcp:db-password@my-staging}}"   # project ID
```

`impersonate_service_account` reads as that service account using your own
credentials, which need `roles/iam.serviceAccountTokenCreator` on it.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `project` | See [Which project](#which-project) | Project for short names |
| `impersonate_service_account` | | Service account to act as |

## API usage and permissions

envhaul calls `versions.access` for each secret version, running up to 8 at
once, and checks each payload's CRC32C checksum. The reading account needs
`roles/secretmanager.secretAccessor` on the secret or project. A secret or
version that doesn't exist counts as missing; a permission error stops the
run.
