# Security policy

envhaul handles secrets, so we take security reports seriously.

## Supported versions

Security fixes are released for the latest minor version. Before 1.0, only
the most recent release is supported.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Older releases | No; please upgrade |

## Reporting a vulnerability

**Please do not open a public issue.** Report it privately through
[GitHub private vulnerability reporting](https://github.com/davidtucker/envhaul/security/advisories/new).

Include what you found, how to reproduce it, and the impact you expect. You
can expect:

- an acknowledgement within **3 business days**,
- an initial assessment within **7 days**,
- a fix or mitigation plan within **30 days** for confirmed issues, coordinated
  with you before public disclosure.

We credit reporters in the advisory unless you ask us not to.

## Scope

In scope: anything that could expose secret values, for example through log
output, error messages, file permissions, temporary files or `diff` output;
writing files outside the configured paths; and supply-chain issues in our
releases (binaries, packages, container images, install script).

Out of scope: weaknesses in AWS itself, and secrets exposed because a
generated `.env` file was committed or shared after envhaul wrote it.

## Verifying releases

Every release's `checksums.txt` is signed with Sigstore keyless signing, and
every archive has an SPDX SBOM and SLSA build provenance. See
[Verifying releases](https://davidtucker.github.io/envhaul/security/#verifying-releases).
