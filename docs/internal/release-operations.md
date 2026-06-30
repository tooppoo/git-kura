# Release Operations

This document describes the maintainer-facing release support workflow for git-kura release steps that are implemented outside the shipped `git kura` CLI. The release support entry point is `go run ./scripts/release ...`.

Winget release operation is not covered yet. This document covers the implemented `tag` and `scoop` steps, plus the `release-asset` validate-only step that the Scoop step depends on.

## Command model

Every executable step follows the same sequence:

```sh
go run ./scripts/release plan --version v0.0.7 --step <step>
go run ./scripts/release validate --version v0.0.7 --step <step>
go run ./scripts/release exec --version v0.0.7 --step <step>
```

The same command form works from Unix shells and from Windows shells when Go is installed and the repository is the current working directory.

The `plan` command writes the intended operation to `.git-kura/release/<version>/<step>/plan.json`. The `validate` command reads that plan, checks it, and writes `.git-kura/release/<version>/<step>/validate-result.json`. The `exec` command refuses to run unless the validate result exists, has `status: "success"`, and still matches the current plan.

`.git-kura/release/` is a local operation artifact directory. It is not committed to git, and it may contain enough local state to explain which release plan was validated or executed. Do not put tokens, secrets, or credentials in plan files, validate results, previews, command output, or PR text.

Each plan has a `planId` and a `payloadHash`. The validate result records both. If the plan is regenerated, edited, or replaced, `exec` detects the mismatch and fails until `validate` is run again for the current plan. `exec` also runs the step preflight immediately before making changes, so state that drifted after validation can still stop the operation.

## Recommended order

Run the implemented non-Winget release operations in this order:

1. `tag`
2. `release-asset` validate
3. `scoop`

The `tag` step creates and pushes the release tag. The release workflow attached to that tag publishes the GitHub Release assets. The `release-asset` step then verifies those assets before package-manager steps use their URLs and checksums. The `scoop` step updates the external Scoop bucket manifest from the validated GitHub Release asset metadata.

## Tag step

The tag step creates an annotated git tag for the target version and pushes it to `origin`.

```sh
go run ./scripts/release plan --version v0.0.7 --step tag
go run ./scripts/release validate --version v0.0.7 --step tag
go run ./scripts/release exec --version v0.0.7 --step tag
```

Validation checks that the main repository worktree is clean, the current branch is `main`, `HEAD`, local `main`, and remote `origin/main` point to the same commit, and the target tag does not already exist locally or remotely.

If tag execution fails while pushing, do not blindly rerun `exec`. First inspect whether the local tag, the remote tag, or both exist:

```sh
git tag -l v0.0.7
git ls-remote origin refs/tags/v0.0.7
```

If the local tag exists but the remote tag does not, delete the local tag only after confirming it is safe, then rerun the full `plan -> validate -> exec` sequence. Never force-push a release tag as part of this recovery.

## Release-Asset Validate Step

`release-asset` is validate-only. It has no external side effects and `exec` is intentionally not part of the workflow.

```sh
go run ./scripts/release plan --version v0.0.7 --step release-asset
go run ./scripts/release validate --version v0.0.7 --step release-asset
```

Validation checks the GitHub Release for the expected platform archives, `checksums.txt`, `checksums.txt.sigstore.json`, SBOM files, tools archive, tools sidecar manifest, and the Windows archives used by package-manager steps. The Windows amd64 and arm64 archive URLs and checksum entries are recorded as package-manager asset data so `scoop` can independently validate the release asset URL and checksum it writes into the manifest.

Because this step reads GitHub Release metadata, run it after the tag-triggered release workflow has completed and the release assets are visible.

## Scoop step

The Scoop step operates on an external Scoop bucket repository checkout. The bucket repository path is passed with `--bucket` and must point to the repository root, not the `bucket/` subdirectory.

```sh
go run ./scripts/release plan --version v0.0.7 --step scoop --bucket "$HOME/catalog-scoop-bucket"
go run ./scripts/release validate --version v0.0.7 --step scoop --bucket "$HOME/catalog-scoop-bucket"
go run ./scripts/release exec --version v0.0.7 --step scoop --bucket "$HOME/catalog-scoop-bucket"
```

The same commands can be run from Windows by using a Windows path for `--bucket`, for example:

```powershell
go run ./scripts/release plan --version v0.0.7 --step scoop --bucket "$HOME\catalog-scoop-bucket"
go run ./scripts/release validate --version v0.0.7 --step scoop --bucket "$HOME\catalog-scoop-bucket"
go run ./scripts/release exec --version v0.0.7 --step scoop --bucket "$HOME\catalog-scoop-bucket"
```

Validation checks that the bucket path is a clean git worktree at its repository root, `bucket/git-kura.json` exists, the manifest has both `64bit` and `arm64` architecture entries, the target Windows release archives exist in the GitHub Release, and `checksums.txt` contains sha256 entries for those archives. `--bucket` must match the path captured in the plan; using a different checkout at validate or exec time fails.

Execution updates only `bucket/git-kura.json`: it sets the manifest version to the target version without the leading `v`, and it writes the GitHub Release `browser_download_url` and sha256 checksum for the Windows `64bit` and `arm64` archives. After writing, the step verifies that the bucket repository diff contains no paths other than `bucket/git-kura.json`, prints that diff, and prints the Scoop validation commands to run.

The current Scoop implementation does not create a branch, commit, push, or GitHub pull request. After `exec`, review the printed diff and run the printed validation commands before creating the bucket repository PR manually. The step does not commit directly to the bucket repository `main` branch.

Expected manual checks after `scoop exec` include:

```sh
git -C "$HOME/catalog-scoop-bucket" diff -- bucket/git-kura.json
pwsh -NoProfile -File "$HOME/catalog-scoop-bucket/bin/checkver.ps1" git-kura
pwsh -NoProfile -File "$HOME/catalog-scoop-bucket/bin/test.ps1" git-kura
scoop install "$HOME/catalog-scoop-bucket/bucket/git-kura.json"
scoop uninstall git-kura
```

When the manual PR is created, rely on the bucket repository's branch protection for force-push restrictions, direct-update restrictions, PR requirement, and code scanning. Waiting for code scanning is outside the current `scoop` step because this implementation stops at manifest update and local validation guidance.

## Devcontainer bucket setup

The devcontainer setup helper can prepare the external bucket repository:

```sh
scripts/dev/setup-bucket-repo.sh
```

By default it uses `$HOME/catalog-scoop-bucket` and `https://github.com/tooppoo/catalog-scoop-bucket.git`. Override those with `GIT_KURA_SCOOP_BUCKET_DIR`, `GIT_KURA_SCOOP_BUCKET_URL`, and `GIT_KURA_SCOOP_BUCKET_REPO` when needed.

This setup is intentionally limited to clone and existence checks. It verifies an existing directory before trusting it, does not destroy or repair existing directories, does not commit, push, or create PRs, and does not generate, store, or print tokens or secrets.

## Failure inspection

To see how far a release operation got, inspect the local operation artifacts and the external systems touched by the step:

```sh
find .git-kura/release/v0.0.7 -maxdepth 3 -type f -print
cat .git-kura/release/v0.0.7/tag/validate-result.json
cat .git-kura/release/v0.0.7/scoop/validate-result.json
git tag -l v0.0.7
git ls-remote origin refs/tags/v0.0.7
git -C "$HOME/catalog-scoop-bucket" status --short
git -C "$HOME/catalog-scoop-bucket" diff -- bucket/git-kura.json
```

A successful validate result means only that the step was safe at validation time. If `exec` fails, inspect the step-specific external state before retrying. Retrying should normally start from a fresh `plan -> validate -> exec` sequence so the `planId`, `payloadHash`, and preflight checks describe the exact operation being executed.
