# Release Operations

This document describes the maintainer-facing release support workflow for git-kura release steps that are implemented outside the shipped `git kura` CLI. The release support entry point is `go run ./scripts/release ...`.

This document covers the implemented `tag` and `homebrew` steps, plus the optional `release-asset` validate-only step that can be run to cross-check GitHub Release assets before package-manager steps. Scoop bucket manifest updates are no longer a git-kura release step; they are handled by the `catalog-scoop-bucket` repository's own workflow (see the Scoop bucket update section below).

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

Run the implemented release operations in this order:

1. `tag`
2. `release-asset` validate
3. `homebrew`

The `tag` step creates and pushes the release tag. The release workflow attached to that tag publishes the GitHub Release assets. The `release-asset` step can then be used to verify those assets before running package-manager steps. The `homebrew` step updates the external Homebrew tap formula; it fetches and validates the needed GitHub Release asset URLs and sha256 checksums itself. Run it after the release workflow has published assets. The Scoop bucket manifest update runs separately in the `catalog-scoop-bucket` repository's own workflow and is not part of this sequence.

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

Validation checks the GitHub Release for the expected platform archives, `checksums.txt`, `checksums.txt.sigstore.json`, SBOM files, tools archive, tools sidecar manifest, and the Windows archives used by package-manager consumers. The Windows amd64 and arm64 archive URLs and checksum entries are recorded as package-manager asset data so the external Scoop bucket repository workflow can reference the release asset URL and checksum with architecture granularity.

Because this step reads GitHub Release metadata, run it after the tag-triggered release workflow has completed and the release assets are visible.

## Scoop bucket update

git-kura's release script no longer updates the Scoop bucket manifest. The responsibility for updating `bucket/git-kura.json` in the `catalog-scoop-bucket` repository — computing the fixed version, `browser_download_url`, and sha256 checksum, creating the update branch, and opening the pull request — now lives in that repository's own workflow. git-kura does not know about the bucket manifest JSON structure, the bucket repository's branches, or its PR-creation flow.

After the release workflow has published the GitHub Release assets, run the `catalog-scoop-bucket` repository's manifest update workflow to produce the bucket update PR. See the bucket repository for its own operating instructions:

- <https://github.com/tooppoo/catalog-scoop-bucket>

## Homebrew step

The Homebrew step operates on an external Homebrew tap repository checkout (`tooppoo/homebrew-tap-catalog`). The tap repository path is passed with `--tap` and must point to the repository root, which is where the tap's `Formula/` directory lives.

```sh
go run ./scripts/release plan --version v0.0.7 --step homebrew --tap "$HOME/homebrew-tap-catalog"
go run ./scripts/release validate --version v0.0.7 --step homebrew --tap "$HOME/homebrew-tap-catalog"
go run ./scripts/release exec --version v0.0.7 --step homebrew --tap "$HOME/homebrew-tap-catalog"
```

Validation checks that the tap path is a clean git worktree at its repository root, that `Formula/git-kura.rb` exists, that the target macOS arm64 and amd64 archives exist in the GitHub Release, and that `checksums.txt` contains sha256 entries for those archives. `--tap` must match the path captured in the plan; using a different checkout at validate or exec time fails. The formula path is fixed at `Formula/git-kura.rb` and is not configurable.

The `homebrew` step updates an existing formula; it does not create the initial `Formula/git-kura.rb`. Seeding the first formula, including its class name, `desc`, `homepage`, `install`, and `test do` blocks, is the tap repository's responsibility. The step relies on the Homebrew convention that each architecture block has a `url "..."` line immediately followed by its `sha256 "..."` line, and that a single `version "..."` line exists; each target must match exactly once or exec fails without writing.

Execution updates only `Formula/git-kura.rb`: it sets the formula version to the target version without the leading `v`, and it writes the GitHub Release `browser_download_url` and sha256 checksum for the macOS arm64 and amd64 archives. After writing, the step verifies that the tap repository diff contains no paths other than `Formula/git-kura.rb`, prints that diff, and prints the Homebrew validation commands to run.

The Homebrew step does not create a branch, commit, push, or GitHub pull request. After `exec`, review the printed diff and run the printed validation commands before creating the tap repository PR manually.

Expected manual checks after `homebrew exec` include:

```sh
git -C "$HOME/homebrew-tap-catalog" diff -- Formula/git-kura.rb
brew install ./Formula/git-kura.rb
brew test git-kura
brew audit --strict --online --formula ./Formula/git-kura.rb
brew uninstall git-kura
```

The `brew install`, `brew audit`, and related commands are run from inside the tap repository checkout. Building `git-kura` archives and running `brew` are macOS operations, so run these checks on macOS even though `plan`, `validate`, and `exec` themselves run anywhere Go is available.

## Failure inspection

To see how far a release operation got, inspect the local operation artifacts and the external systems touched by the step:

```sh
find .git-kura/release/v0.0.7 -maxdepth 3 -type f -print
cat .git-kura/release/v0.0.7/tag/validate-result.json
cat .git-kura/release/v0.0.7/homebrew/validate-result.json
git tag -l v0.0.7
git ls-remote origin refs/tags/v0.0.7
git -C "$HOME/homebrew-tap-catalog" status --short
git -C "$HOME/homebrew-tap-catalog" diff -- Formula/git-kura.rb
```

A successful validate result means only that the step was safe at validation time. If `exec` fails, inspect the step-specific external state before retrying. Retrying should normally start from a fresh `plan -> validate -> exec` sequence so the `planId`, `payloadHash`, and preflight checks describe the exact operation being executed.
