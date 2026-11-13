# Releasing

Releases use [release-please](https://github.com/googleapis/release-please) and
[GoReleaser](https://goreleaser.com). Tags are not created by hand.

## Flow

1. Pull requests are squash-merged. The PR title becomes the commit message on `main`.
2. On each push to `main`, release-please opens or updates a release pull request
   (`chore: release X.Y.Z`) containing the next version and the `CHANGELOG.md` section.
3. Merging the release pull request creates the tag and a draft GitHub release. The changelog
   section is the release notes.
4. The `Release` workflow builds and signs the artifacts with GoReleaser, uploads them to the
   draft and publishes it. The release is not public until its assets are attached.

## Pull request titles

Format: `type(scope): description`. The description starts with a lowercase letter. The
`PR Title` check enforces it.

```
feat(vcs): add write-only client_secret_wo
fix(workspace): keep module_ssh_key on update
feat(variable)!: mark value as sensitive
```

| Type | Changelog section | Bump while `0.x` |
|---|---|---|
| `feat` | Features | minor |
| `fix` | Bug Fixes | patch |
| `perf` | Performance Improvements | patch |
| `revert` | Reverts | patch |
| `refactor` | Code Refactoring | patch |
| `docs`, `test`, `build`, `ci`, `chore` | hidden | none |

A breaking change has `!` after the type, or a `BREAKING CHANGE:` footer in the squash commit
message. It is listed in its own section of the release notes and bumps the minor version
while the provider is `0.x` (the major version from `1.0.0`). The commit body states what
users have to change, since it goes into the release notes.

## Versioning

[Semantic versioning](https://semver.org). While `0.x`, features and breaking changes go in a
minor release and a patch release contains only bug fixes. A patch release must not break an
existing plan.

To force a version, add a footer to a commit on `main`:

```
Release-As: 1.0.0
```

## Cutting a release

1. Review the generated changelog in the release pull request. Reword a line by editing the
   release pull request.
2. Check that the acceptance tests on `main` pass.
3. Merge the release pull request.

If the `Publish` job fails, fix the cause and run `Release` from the Actions tab with the tag
(for example `v0.32.0`). The tag and the draft release already exist, so it can run again.

## Fix release from a release branch

For a fix to an older version without the changes merged to `main` since.

```sh
git switch -c release/0.31 v0.31.0
git push origin release/0.31
```

For each fix:

1. Merge the fix to `main` with a `fix:` title.
2. Open a pull request against `release/0.31` that cherry-picks it, with the same title:
   ```sh
   git switch -c backport/my-fix origin/release/0.31
   git cherry-pick -x <sha-on-main>
   ```
3. Merge the `chore: release 0.31.1` pull request that release-please opens against
   `release/0.31`.

The `Release Please` workflow runs on every `release/**` branch and reads the manifest on
that branch. Create the branch after the automation is on `main`. After a release from a
branch, copy its `CHANGELOG.md` section to `main` in a pull request.

## Repository settings

- Squash merge is the only merge method, with the default commit message set to the pull
  request title.
- Settings > Actions > General > Workflow permissions: "Allow GitHub Actions to create and
  approve pull requests" is enabled.
- `release/**` has the same branch protection as `main`.
- Secrets `GPG_PRIVATE_KEY` and `PASSPHRASE` sign the checksums.
- Pull requests opened with `GITHUB_TOKEN` do not start other workflows, so `Tests` does not
  run on the release pull request. It changes only `CHANGELOG.md` and the manifest. Exclude it
  from required checks, or use a GitHub App token for release-please.
