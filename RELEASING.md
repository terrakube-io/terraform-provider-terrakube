# Releasing

Releases are automated with [release-please](https://github.com/googleapis/release-please)
and [GoReleaser](https://goreleaser.com). Nobody tags by hand.

## How it works

1. Every pull request is **squash-merged**, and its title becomes the commit message on `main`.
2. On each push to `main`, release-please opens or updates a **release pull request**
   ("chore: release 0.32.0") with the next version and the new `CHANGELOG.md` section.
3. Merging the release pull request creates the tag and a **draft** GitHub release with the
   changelog as its notes.
4. The `Release` workflow checks out the tag, builds and signs the artifacts with GoReleaser,
   uploads them to the draft and publishes it. The Terraform Registry picks it up from there.
   The release is never public without its assets.

## Pull request titles

The title must be a conventional commit: `type(scope): description`, with a lowercase
description. The `PR Title` check enforces it. Examples:

- `feat(vcs): add write-only client_secret_wo`
- `fix(workspace): keep module_ssh_key on update`
- `feat(variable)!: mark value as sensitive`

| Type | Changelog section | Version bump while 0.x |
|---|---|---|
| `feat` | Features | minor |
| `fix` | Bug Fixes | patch |
| `perf` | Performance Improvements | patch |
| `revert` | Reverts | patch |
| `refactor` | Code Refactoring | patch |
| `docs`, `test`, `build`, `ci`, `chore` | hidden | none |

A **breaking change** gets a `!` after the type (`fix(variable)!:`) or a `BREAKING CHANGE:`
footer in the squash commit message. It is listed in its own section at the top of the
release notes and bumps the **minor** version while the provider is `0.x` (and the major
version from `1.0.0`). Write what users have to change in the commit body, because it goes
into the release notes.

## Versioning

The provider follows [semantic versioning](https://semver.org). While it is `0.x`, a minor
version is where new features and breaking changes ship, and a patch version only has bug
fixes. A patch release must never break an existing plan.

To force a version, add a footer to a commit message on `main`:

```
Release-As: 1.0.0
```

## Cutting a release

1. Open the release pull request and read the changelog it generated. Edit the pull request
   description or the commit titles on `main` if a line needs rewording.
2. Make sure the acceptance tests on `main` are green.
3. Merge it. The `Release` workflow publishes the release.

If the `Publish` job fails, fix the cause and run `Release` from the Actions tab with the tag
(for example `v0.32.0`). The tag and the draft already exist, so it is safe to run again.

## Bug fix releases from a release branch

Use this to ship a fix for an older version without taking what has landed on `main` since.

```sh
git switch -c release/0.31 v0.31.0
git push origin release/0.31
```

Then, for each fix:

1. Merge the fix to `main` with a `fix:` title as usual.
2. Cherry-pick it onto the release branch with a pull request targeting `release/0.31`:
   ```sh
   git switch -c backport/my-fix origin/release/0.31
   git cherry-pick -x <sha-on-main>
   ```
   Give the pull request the same conventional title as the original.
3. release-please opens "chore: release 0.31.1" against `release/0.31`. Merge it to publish.

The `Release Please` workflow runs on every `release/**` branch and uses the manifest on that
branch, so the branch must be created after this automation is on `main`. After a backport
release, copy its section of `CHANGELOG.md` to `main` in a pull request so the history stays
complete.

Protect `release/**` in the repository settings the same way as `main`.

## Repository settings this relies on

- **Squash merge** is the only merge method, with the default commit message set to the
  pull request title.
- **Settings > Actions > General > Workflow permissions**: "Allow GitHub Actions to create
  and approve pull requests" is enabled, so release-please can open the release pull request.
- **Secrets**: `GPG_PRIVATE_KEY` and `PASSPHRASE` for signing.
- Pull requests opened with the default `GITHUB_TOKEN` do not start other workflows, so the
  `Tests` checks do not run on the release pull request. It only changes `CHANGELOG.md` and
  the manifest, so exclude it from required checks, or switch release-please to a GitHub App
  token if you want them.
