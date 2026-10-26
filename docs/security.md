# Security

## How envhaul handles secrets

- **Only references are committed.** Secret values stay in AWS until you run
  a command that resolves them.
- **Generated files are private.** They're written with mode `0600`
  atomically (temp file, then rename), so a reader never sees a partial file.
  `sync` and `validate` warn when a generated file isn't git-ignored, and
  `init` offers to add it to `.gitignore`.
- **Values are never logged.** `--verbose` logs which parameters and secrets
  are requested, never their values, and error messages name references, not
  values. The integration test checks that verbose output contains no
  secret values.
- **`diff` masks values** unless you pass `--show-values`.
- **`exec` skips the disk entirely.** Values go straight into the child
  process's environment.
- **Read-only AWS access.** envhaul only calls `ssm:GetParameters` and
  `secretsmanager:GetSecretValue`.

Environment variables are visible to other processes running as the same
user, and `.env` files are plaintext. envhaul makes them easy to manage but
doesn't change that; use it on machines you trust.

## Verifying releases

Every release publishes `checksums.txt`, a Sigstore bundle for it, an SPDX
SBOM per archive, and SLSA build provenance.

Verify the checksums file was signed by this repository's release workflow:

```sh
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/davidtucker/envhaul/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

Verify an archive's build provenance with the GitHub CLI:

```sh
gh attestation verify envhaul_0.1.0_linux_amd64.tar.gz --repo davidtucker/envhaul
```

The same attestation is attached to each release as
`envhaul_<version>.intoto.jsonl`, so you can also verify offline:

```sh
gh attestation verify envhaul_0.1.0_linux_amd64.tar.gz \
  --bundle envhaul_0.1.0.intoto.jsonl --repo davidtucker/envhaul
```

Container images are signed too:

```sh
cosign verify ghcr.io/davidtucker/envhaul:latest \
  --certificate-identity-regexp '^https://github.com/davidtucker/envhaul/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## Reporting vulnerabilities

See [SECURITY.md](https://github.com/davidtucker/envhaul/blob/main/SECURITY.md).
Please report privately through GitHub, not in a public issue.
