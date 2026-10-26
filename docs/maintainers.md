# Maintainer guide

## Releasing

1. Merge PRs with Conventional Commit titles. release-please keeps a
   **chore: release X.Y.Z** PR open with the version bump and changelog.
2. Review that PR's changelog, then merge it. release-please tags `vX.Y.Z`
   and creates the GitHub Release.
3. The `release` job runs GoReleaser, which builds and signs binaries,
   packages, SBOMs and the container image, attests provenance, and opens
   commits on the Homebrew tap and Scoop bucket. The tap and bucket are
   skipped while their tokens aren't configured.
4. Check the release page. Verify one artifact with the commands in
   [Security](security.md#verifying-releases).
5. The last release step requests the new version from `proxy.golang.org`,
   so `go install …@latest` and [pkg.go.dev](https://pkg.go.dev/github.com/davidtucker/envhaul)
   pick it up within minutes.

Stable releases come only from merging the release PR. Pushing a stable tag
(`vX.Y.Z`) yourself does nothing, so release-please's tag and its workflow
can't publish the same version twice.

### Retrying a failed release

If the release job fails after the tag exists (tags can't be moved or
deleted), fix the cause on `main` if needed, then run **Actions → Release →
Run workflow** with the tag, e.g. `v0.1.0`. GoReleaser replaces any assets
that were already uploaded.

### Release candidates

Push a pre-release tag yourself to exercise the pipeline without publishing
to Homebrew or Scoop:

```sh
git tag -s v0.2.0-rc.1 -m "v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

Before the first real release, cut `v0.1.0-rc.1` to check signing,
attestations, the provenance upload and the image push end to end. The tap and
bucket are only exercised by stable releases.

### The release-please GitHub App

release-please runs as a small GitHub App (`envhaul release`) rather than with
the workflow's `GITHUB_TOKEN`. GitHub doesn't run CI on PRs opened with
`GITHUB_TOKEN` until a maintainer approves each run, and never triggers
`pull_request_target` checks for them, so the release PR could never satisfy
the required checks.

The App needs these repository permissions: **Contents: read and write**,
**Pull requests: read and write** and **Issues: read and write** (for
labels). It is installed only on this repository. The workflow reads:

| Name | Kind | Value |
| --- | --- | --- |
| `RELEASE_PLEASE_APP_CLIENT_ID` | Actions **variable** | The App's Client ID |
| `RELEASE_PLEASE_APP_PRIVATE_KEY` | Actions **secret** | A private key generated for the App (the whole `.pem` file) |

Without them, release-please falls back to `GITHUB_TOKEN` and you'd have to
approve the release PR's CI runs by hand. Rotate the private key by generating
a new one in the App's settings, updating the secret, then deleting the old
key.

### Building locally

```sh
make release-check   # validate .goreleaser.yaml
make snapshot        # build every artifact into ./dist without publishing
```

## Dependency updates

Dependabot (`.github/dependabot.yml`) opens weekly or monthly update PRs for
Go modules, GitHub Actions, the Docker images and the docs tooling.

- **7-day age gate.** A new version is only proposed once it has been
  published for 7 days (`cooldown`), which gives the ecosystem time to catch
  compromised or broken releases. **Security updates skip the gate** and
  arrive as soon as an advisory is published.
- **Overriding the gate.** To exempt a dependency permanently, add it to
  that ecosystem's `cooldown.exclude` list. For a one-off urgent update, bump
  it yourself in a PR.
- **Auto-merge.** `.github/workflows/dependabot-automerge.yml` turns on
  auto-merge for updates up to `AUTOMERGE_UP_TO` (`none`, `patch` or `minor`;
  currently `minor`). They merge only after every required check passes;
  anything that fails stays open for you. **Major updates are never
  auto-merged**, and a grouped PR is judged by its largest change.
- Go module and Docker updates use the `fix(deps)` prefix, so release-please
  proposes a patch release that ships them. Actions, test and docs updates
  don't trigger releases.

## One-time repository setup

These settings live in GitHub, not in the repository.

**Repositories**

- [ ] Create the release-please GitHub App and set
      `RELEASE_PLEASE_APP_CLIENT_ID` / `RELEASE_PLEASE_APP_PRIVATE_KEY`
      (see [The release-please GitHub App](#the-release-please-github-app)).
- [ ] Create `davidtucker/homebrew-tap` (with a `Casks/` directory) and
      `davidtucker/scoop-bucket`.
- [ ] Create fine-grained tokens with *Contents: read and write* on each, and
      add them as the `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` Actions
      secrets.
- [ ] Sign in at [codecov.io](https://codecov.io) with GitHub and enable
      `davidtucker/envhaul`. Uploads authenticate with GitHub OIDC, so no
      `CODECOV_TOKEN` is needed. The `Coverage` check isn't required, so a
      Codecov problem never blocks merges.

**Settings → General**

- [ ] Upload `.github/assets/social-preview.png` as the social preview.
- [ ] Description: *Haul config and secrets from AWS into .env files.*
      Website: `https://davidtucker.github.io/envhaul`. Topics: `dotenv`,
      `env`, `aws`, `ssm`, `secrets-manager`, `cli`, `go`, `devtools`.
- [x] Pull requests: allow **squash merging** only, with the **PR title** as
      the commit title and the commit messages as the body. Enable
      **auto-merge**, **always suggest updating branches** and
      **automatically delete head branches**.
- [ ] Enable **Discussions**.

**Settings → Rules → Rulesets**

- [x] **`main`** (default branch), with no bypass actors:
    - Require a pull request: **0 approvals** while there is a single
      maintainer (GitHub doesn't let you approve your own PR), stale
      approvals dismissed, conversations resolved, squash merge only.
      Raise it to 1 when a second maintainer joins.
    - Require status checks, with the branch up to date:
      the six `Test (…)` jobs, `Lint`, `Fuzz`, `Integration (AWS emulator)`,
      `Release dry run`, `Analyze (go)`, `Analyze (actions)`, `govulncheck`
      and `conventional`.
    - Block merges on new CodeQL alerts of high severity or above.
    - Require linear history and signed commits; block force pushes and
      deletion.
- [x] **`release tags`** (`refs/tags/v*`): tags can be created (by
      release-please, or a maintainer pushing an `-rc` tag) but never moved
      or deleted, so a published version always points at the same code.

To change these, edit them under Settings → Rules → Rulesets, or with
`gh api repos/davidtucker/envhaul/rulesets`. If you rename a CI job, update
the required checks in the same change; otherwise PRs wait for a check that
never reports.

The release PR is opened by the release-please GitHub App, so its checks run
like any other PR's. If the App isn't configured, the PR comes from
`github-actions[bot]` and each CI run needs your approval first.

**Settings → Code security**

- [ ] Private vulnerability reporting.
- [ ] Dependabot alerts and security updates. Version updates come from
      `.github/dependabot.yml`.
- [ ] Secret scanning with push protection.
- [ ] Code scanning (CodeQL and Scorecard results are uploaded by workflows).

**Settings → Pages**

- [x] Source: **GitHub Actions**.

**Settings → Actions → General**

- [x] Workflow permissions: **read repository contents** by default, and
      allow GitHub Actions to create and approve pull requests (used by the
      Dependabot auto-merge workflow, and by release-please when the App
      isn't configured).

## Branding

Logos, colors and banners are in `.github/assets/`. The CLI embeds copies of
the banners from `internal/brand/`. If you change a banner, copy it there too
(CI checks that they match).
