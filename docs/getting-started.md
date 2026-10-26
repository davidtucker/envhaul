# Getting started

## Install

=== "Homebrew"

    ```sh
    brew install davidtucker/tap/envhaul
    ```

=== "Scoop"

    ```powershell
    scoop bucket add davidtucker https://github.com/davidtucker/scoop-bucket
    scoop install envhaul
    ```

=== "Go"

    ```sh
    go install github.com/davidtucker/envhaul/cmd/envhaul@latest
    ```

=== "Script"

    ```sh
    curl -fsSL https://raw.githubusercontent.com/davidtucker/envhaul/main/install.sh | sh
    ```

    The script verifies the archive's checksum, and the release signature if
    [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is installed.

=== "deb / rpm / apk"

    Download the package for your platform from
    [GitHub Releases](https://github.com/davidtucker/envhaul/releases), then:

    ```sh
    sudo dpkg -i envhaul_*_amd64.deb     # Debian, Ubuntu
    sudo rpm -i envhaul-*.x86_64.rpm     # Fedora, RHEL
    sudo apk add --allow-untrusted envhaul_*_x86_64.apk  # Alpine
    ```

=== "Docker"

    ```sh
    docker run --rm -v "$PWD:/work" -w /work \
      -v "$HOME/.aws:/home/nonroot/.aws:ro" \
      ghcr.io/davidtucker/envhaul sync
    ```

Check it works:

```sh
envhaul version
```

## Create a config

```sh
envhaul init
```

This writes a commented `envhaul.yaml` and offers to add `.env` to
`.gitignore`. Edit it to list your components and variables:

```yaml
name: dev
aws_profile: my-dev-profile     # from ~/.aws/config
aws_region: us-east-1
ssm_prefix: /myapp/dev

components:
  app:
    env_file: .env
    vars:
      APP_ENV: development                  # inline, committed
      API_KEY: "{{ssm:api/API_KEY}}"        # /myapp/dev/api/API_KEY
      DB_PASSWORD: "{{secret:myapp/dev/db#password}}"
```

## Validate, preview, sync

```sh
envhaul validate     # offline: syntax, names, references
envhaul list         # offline: where each value comes from
envhaul diff         # fetch and show what would change (values masked)
envhaul sync         # fetch and write the .env files
```

If your profile uses IAM Identity Center (SSO), sign in first:

```sh
aws sso login --profile my-dev-profile
```

## Next steps

- [Configuration](config.md): every config option
- [References and pipes](references.md): the template syntax
- [AWS profiles and permissions](aws.md): multiple accounts and IAM policies
- Shell completion: `envhaul completion --help` (Homebrew and the Linux
  packages install it for you)
