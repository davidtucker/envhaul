# Maintainer guide

## Releasing

1. Merge PRs with Conventional Commit titles. release-please keeps a
   **chore: release X.Y.Z** PR open with the version bump and changelog.
2. Review that PR's changelog, then merge it. release-please tags `vX.Y.Z`
   and creates the GitHub Release.
3. The `release` job runs GoReleaser, which builds and signs binaries,
   packages, SBOMs and the container image, attests provenance, and opens
   commits on the Homebrew tap and Scoop bucket.
4. Check the release page. Verify one artifact with the commands in
   [Security](security.md#verifying-releases).

### Release candidates

Push a pre-release tag yourself to exercise the pipeline without publishing
to Homebrew or Scoop:

```sh
git tag -s v0.2.0-rc.1 -m "v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

Before the first real release, cut `v0.1.0-rc.1` to check signing,
attestations, the image push and the tap/bucket tokens end to end.

### Building locally

```sh
make release-check   # validate .goreleaser.yaml
make snapshot        # build every artifact into ./dist without publishing
```

## One-time repository setup

These settings live in GitHub, not in the repository.

**Repositories**

- [ ] Create `davidtucker/homebrew-tap` (with a `Casks/` directory) and
      `davidtucker/scoop-bucket`.
- [ ] Create fine-grained tokens with *Contents: read and write* on each, and
      add them as the `HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` Actions
      secrets.
- [ ] Optional: add `CODECOV_TOKEN` after enabling the repository on Codecov.

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

Pull requests opened by `github-actions[bot]` (the release PR) only run
workflows after a maintainer approves them. Approve the runs on the release
PR before merging, or give release-please a GitHub App or fine-grained token
so its PRs run CI automatically.

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
      allow GitHub Actions to create and approve pull requests (needed by
      release-please).

## Branding

Logos, colors and banners are in `.github/assets/`. The CLI embeds copies of
the banners from `internal/brand/`. If you change a banner, copy it there too
(CI checks that they match).
