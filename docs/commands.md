# Commands

`git-kura` is invoked as a Git subcommand:

```sh
git kura <command> [arguments]
```

`git-kura` provides following commands.

```sh
git kura open fizz              # create a worktree and branch for key "fizz"
git kura open fizz --dry-run    # print the worktree that would be created
git kura get fizz               # print the open worktree path for "fizz"
git kura get fizz --path        # print the worktree path for "fizz"
git kura get fizz --branch      # print the branch name for "fizz"
git kura get fizz --root        # print the repository root path
git kura get fizz --format json # print workspace metadata as JSON
git kura get fizz --json        # alias of `--format json`
git kura get fizz --format toon # print workspace metadata as TOON for AI prompts
git kura get fizz --toon        # alias of `--format toon`
git kura close fizz             # remove the worktree for "fizz"
git kura ls                     # list all open worktrees
git kura seal claim <path...>   # claim paths for the current seal key
git kura seal unclaim <path...> # release the current seal key's claim on paths
git kura seal test <path...>    # check paths against the current seal context
git kura seal ls [key]          # list claimed paths (project-wide by default)
git kura seal doctor            # validate the project-wide seal store
git kura tools status            # show the install state of every component
git kura tools status pre-commit # show the install state of one component
git kura tools install <comp...> # install components from the matching release asset
git kura tools install --all     # install every component
git kura tools uninstall <comp...> # remove components
git kura tools uninstall --all   # remove every component
```

## `git kura open <key>`

Create the branch and worktree for the given key.

```sh
git kura open 51
git kura open 51 --dry-run
git kura open 51 --dry-run --json
```

If the corresponding branch or worktree already exists, Kura should not create a conflicting workspace.

`--dry-run` does not create the branch, worktree, or metadata. On its own it prints human-readable output showing the planned worktree path, branch, repository root, and base branch. With `--json` it prints the common output envelope instead, with the planned worktree under `data`.

A dry run also checks, without side effects, conditions that would collide at real creation time: an existing worktree path, branch, or metadata. Such a conflict does not fail the command; the dry run still succeeds with exit code `0` and reports each conflict as a warning (`open-dry-run-conflict`). In JSON mode the warnings appear in `warnings[]` with the colliding items under `details.conflicts`.

## `git kura get <key>`

Resolve the branch or worktree associated with the given key.

```sh
git kura get 51
git kura get 51 --path
git kura get 51 --branch
git kura get 51 --root
git kura get 51 --json
git kura get 51 --toon
```

`git kura get <key>` and all output flags require the key to be currently open.
Use `git kura open <key> --dry-run` when you want to inspect the path and branch that would be created.

This command is designed for both humans and scripts.

For example:

```sh
codex review "$(git kura get 51)"
```

`--root` prints the repository root path. This is useful for scripts that need to locate files relative to the repository while operating inside a worktree:

```sh
root="$(git kura get 51 --root)"
```

See [output-format.md](output-format.md) for the full metadata schema and output format reference.

## `git kura close <key>`

Remove the worktree and Kura-managed branch associated with the given key, and release every path seal that the key holds in the repository-wide seal store. This keeps a closed worktree from leaving stale claims that would block other worktrees from claiming the same paths.

```sh
git kura close 51
```

Kura should refuse to remove a worktree when doing so would discard uncommitted changes unless explicitly instructed.

`close` takes the seal store lock (`paths.lock`) at the start, before any cleanup, so that releasing the key's seals is atomic with removing its worktree, branch, and metadata. If the lock cannot be acquired within the retry timeout, `close` fails with `seal-lock-timeout` (exit code 5) and changes nothing.

After taking the lock, `close` reads and validates `paths.json` before any destructive cleanup. An absent `paths.json` is treated as an empty seal store and cleanup continues. If `paths.json` cannot be read or does not conform to the seal store schema, `close` does not start cleanup and leaves the worktree, branch, `paths.json`, and metadata unchanged. Only the seals whose `entry.key` equals the closed key are removed; claims held by other keys are left untouched.

## `git kura ls [--json]`

List all currently open worktrees managed by Kura.

```sh
git kura ls
git kura ls --json
```

Without `--json`, prints one key per line to standard output, sorted alphabetically. If no worktrees are currently open, the output is empty and the exit code is 0.

This command is designed for scripts and enumeration:

```sh
for key in $(git kura ls); do
  git kura get "$key" --toon
done
```

With `--json`, emits a [common output envelope](adr/20260617T070134Z_output-framework-envelope-result-renderer.md) whose `data` field is:

```json
{
  "keys": ["51", "62"]
}
```

`keys` is a sorted string array of the open worktree keys, or `[]` when none are open. `ls --json` returns key names only; for per-key details use `git kura get <key> --json`. See [`docs/adr/20260618T145556Z_ls-json-returns-key-enumeration-only.md`](adr/20260618T145556Z_ls-json-returns-key-enumeration-only.md) for the rationale.

## Keys

A key is an opaque, case-sensitive string identifier.

Kura does not parse keys as numbers. Kura does not resolve keys through GitHub, GitLab, or any issue tracker.

Key must match:

```txt
[A-Za-z0-9][A-Za-z0-9._-]{0,127}
```

Additionally, Kura rejects keys that:

* are `"."` or `".."`
* contain `".."`
* start with `"."`
* end with `"."`
* end with `".lock"`
* contain path separators `"/"` or `"\"`
* contain whitespace
* contain control characters
* contain shell metacharacters
* contain Git ref expression syntax such as `"@{"`

## Seal commands

`git kura seal` manages *path seals* scoped to a seal key.
The command reference is below. For the concepts behind these commands — how they are classified, the meaning of *project scope*, and which commands depend on the current seal key — see [Seal commands: context and scope](commands/seal-commands.md).

## `git kura seal claim <path> [path...]`

Claim one or more repository-relative file paths for the current key in the seal store. Claiming records that the current task intends to edit those paths so conflicting edits across tasks/worktrees are detected before merge.

The current key is derived from the git-kura managed worktree you run the command in. Move into the worktree created by `git kura open <key>` and that worktree's key becomes the current key:

```sh
cd "$(git kura get issue-18)"
git kura seal claim src/foo.go
git kura seal claim src/foo.go tests/foo_test.go
```

`seal claim` fails when it is not run inside a managed worktree, or when that worktree's metadata is missing or inconsistent.

Paths are interpreted relative to the repository root regardless of the current working directory; absolute paths are rejected. All paths are validated before any change is written — if one path fails, the store is not modified.

Exits with `seal-conflict` (code 6) if any path is already claimed by a different key. Exits with `seal-lock-timeout` (code 5) if the store lock cannot be acquired within the retry timeout.

## `git kura seal unclaim <path> [path...]`

Release the current key's claim on one or more file paths in the seal store.

```sh
git kura seal unclaim src/foo.go
git kura seal unclaim src/foo.go tests/foo_test.go
```

Only the key that claimed a path may release it. Attempting to unclaim a path claimed by a different key exits with `seal-conflict` (code 6). Paths not currently claimed are silently skipped (idempotent).

## `git kura seal test [--json] <path> [path...]`

Check whether one or more repository-relative paths may be handled in the current seal context, without modifying the store. `seal test` answers a single question: given the current key, is every listed path safe to edit?

The current key is derived from the git-kura managed worktree you run the command in, exactly like `seal claim` / `seal unclaim`:

```sh
cd "$(git kura get issue-18)"
git kura seal test src/foo.go
git kura seal test src/foo.go tests/foo_test.go
```

`seal test` fails when it is not run inside a managed worktree, or when that worktree's metadata is missing or inconsistent. This context error is distinct from a seal conflict. `GIT_KURA_SEAL_KEY` is **not** consulted for current-key resolution and does not affect the result.

A path is safe when it is unclaimed, or already claimed by the current key. A path claimed by a different key is a conflict regardless of whether the file exists in the current working tree. Paths are interpreted relative to the repository root regardless of the current working directory; absolute paths and paths outside the repository are rejected. A path inside the repository that does not exist yet and is unclaimed is treated as safe, so `seal test` can check a file before it is created.

`seal test` exits 0 only when every path is safe. If any path conflicts it exits with `seal-conflict` (code 6) and reports each conflicting path with the key that claims it. `seal test` is read-only: it does not modify the store and does not take the store lock, so it is never blocked by a held `paths.lock`.

With `--json`, emits a [common output envelope](adr/20260617T070134Z_output-framework-envelope-result-renderer.md) whose `data` field is:

```json
{
  "currentKey": "issue-18",
  "passed": true,
  "results": [
    { "path": "src/foo.go", "status": "claimed-by-current-key", "safe": true, "claimedBy": "issue-18" },
    { "path": "src/bar.go", "status": "unclaimed",              "safe": true, "claimedBy": null },
    { "path": "new-file.go","status": "missing-path",           "safe": true, "claimedBy": null }
  ]
}
```

`currentKey` is the key derived from the current worktree. `passed` is `true` only when every path is safe. `results` preserves the input path order. Each result item always includes `path`, `status`, `safe`, and `claimedBy`.

`status` values:

| value | meaning | `safe` |
|---|---|---|
| `claimed-by-current-key` | claimed by the current key | `true` |
| `claimed-by-other-key` | claimed by a different key | `false` |
| `unclaimed` | not in the store and the file exists | `true` |
| `missing-path` | not in the store and the file does not exist | `true` |

When `passed` is `false`, `ok` is still `true` — the diagnostic ran successfully and produced a result — but the exit code is `seal-conflict` (6), preserving the existing CLI contract. See [`docs/adr/20260619T150000Z_diagnostic-output-execution-vs-result.md`](adr/20260619T150000Z_diagnostic-output-execution-vs-result.md) for the rationale.

When current key resolution fails, `ok` is `false` and `error.code` is `current-key-unresolved`. `error.details.reason` provides a sub-classification: `not-inside-git-repository`, `not-in-managed-worktree`, `metadata-missing`, or `metadata-inconsistent`. `--json` must appear **before** the path arguments; any flag after the first path is a usage error.

## `git kura seal ls [--json] [key]`

List claimed paths recorded in the seal store, one per line:

```txt
<key>	<path>
```

```sh
git kura seal ls          # every claimed path, across all keys
git kura seal ls issue-18 # only paths claimed by issue-18
```

`ls` is a repository-wide inspection command. Unlike `seal claim` and `seal unclaim`, it does **not** derive a current key from the worktree: its output is the same whether it runs from the main checkout or from inside a managed worktree. To inspect a single key, pass the key as an explicit argument (validated with the same key rules). See [`docs/adr/20260612T170922Z_seal-command-current-context-and-scope.md`](adr/20260612T170922Z_seal-command-current-context-and-scope.md) for the rationale.

The listed scope is the seal store in the Git common dir, shared by all worktrees of the repository. Paths are repository-root relative with `/` separators. Output is sorted by key, then by path within a key.

An absent store, an empty store, or a key with no claimed paths all produce empty output and exit 0. A store that cannot be parsed, has an unsupported `schemaVersion`, or does not match the store schema is an error.

`ls` is read-only and does not take the store lock, so it is never blocked by a held `paths.lock`.

With `--json`, emits a [common output envelope](adr/20260617T070134Z_output-framework-envelope-result-renderer.md) whose `data` field is:

```json
{
  "filterKey": null,
  "claims": [
    { "key": "issue-18", "path": "src/foo.go" }
  ]
}
```

`filterKey` is `null` for project-wide listing and the key string when a key argument is given. `claims` is always present (`[]` when empty). Each claim item always includes both `key` and `path` regardless of whether `filterKey` is set. `--json` must appear **before** the optional key argument; `seal ls <key> --json` is a usage error. See [`docs/adr/20260618T145559Z_seal-ls-json-uses-unified-claim-shape.md`](adr/20260618T145559Z_seal-ls-json-uses-unified-claim-shape.md) for the rationale.

## `git kura seal doctor [--json]`

Validate the project-wide path seal store for the Git repository resolved from the current working directory.

```sh
git kura seal doctor
```

`doctor` is a repository-wide inspection command. It does **not** derive a current key from the worktree, does **not** read git-kura worktree metadata, and does **not** consult `GIT_KURA_SEAL_KEY`. It can run from the main checkout, from a git-kura managed worktree, or from any other directory inside the Git repository.

An absent `paths.json` is treated as an empty store and succeeds. A healthy store exits 0 and prints nothing to stdout.

`doctor` validates the store file structure, `schemaVersion`, entry keys, repository-relative path syntax, `/` path separators, paths that escape the repository root, and paths that would duplicate another entry after normalization. It does not check whether stored paths currently exist in the working tree, whether they are files or directories, or where symlinks point.

If the store is malformed or inconsistent, `doctor` exits with `seal-doctor-error` (code 7) and reports every problematic store entry it finds on stderr, so all issues can be fixed in a single pass. `doctor` is read-only: it does not modify `paths.json`, does not take `paths.lock`, and does not create, remove, or rewrite a lock file.

With `--json`, emits a [common output envelope](adr/20260617T070134Z_output-framework-envelope-result-renderer.md) whose `data` field is:

```json
{
  "healthy": true,
  "summary": { "checkedClaims": 3, "errorCount": 0, "warningCount": 0 },
  "findings": []
}
```

`healthy` is `true` only when no findings are reported. `summary.checkedClaims` is the number of store entries inspected. `findings` is always present (`[]` when healthy). Each finding includes `severity` (`error` or `warning`), `code` (a hyphen-case token identifying the violation), `path` (the offending store path, or `null` for store-level findings), and `message` (a human-readable description).

When `healthy` is `false`, `ok` is still `true` — the store was read and inspected successfully — but the exit code is `seal-doctor-error` (7), preserving the existing CLI contract. When the store cannot be read or parsed, `ok` is `false`, `error.code` is `seal-doctor-error`, and the exit code is also 7. See [`docs/adr/20260619T150000Z_diagnostic-output-execution-vs-result.md`](adr/20260619T150000Z_diagnostic-output-execution-vs-result.md) for the rationale.

## Tools commands

`git kura tools` installs, removes, and inspects *tool components*: self-contained helpers — such as a pre-commit hook or an editor skill — that git-kura installs into the repository from a verified release asset and can later remove.

The framework recognizes three component IDs: `pre-commit`, `claude-skill`, and `codex-skill`. All three are fully implemented.

`git kura tools install claude-skill` installs a SKILL.md file to `<repository-root>/.claude/skills/git-kura/SKILL.md`, teaching a Claude Code agent how to use git-kura safely. `git kura tools install codex-skill` installs the equivalent to `<repository-root>/.agents/skills/git-kura/SKILL.md` for Codex. Both components install files from the tools release archive into the representative root — the stable repository root derived from the git common dir — so the skill files remain in place even when the command is run from inside a managed worktree. Existing user-modified or unmanaged files at the destination are never overwritten; `install` fails instead.

`git kura tools install pre-commit` installs a thin wrapper script and points `core.hooksPath` at the managed hooks directory using the repository-local config scope only. At commit time the hook runs the same path-level seal check as `git kura seal test` against the staged files, chains any previously configured pre-commit hook, and re-checks the staged files after the previous hook finishes. The commit is rejected when any check fails. This is a local safety guard only; `git commit --no-verify` bypasses it entirely. If a higher-precedence `core.hooksPath` (`worktree` or `command` scope) would shadow the local setting, `install` fails before changing anything. `git kura tools uninstall pre-commit` restores `core.hooksPath` to its prior state.

An unknown component is a usage error (exit code 2). `install` and `uninstall` require at least one component or `--all`, and `--all` may not be combined with explicit component names; both violations are usage errors. `status` does not accept `--all`: run it with no component to show every component.

When several components are given, git-kura processes as many as it can: one component that fails does not stop the rest. The command exits non-zero when at least one component failed. A `skipped` or `not-installed` result is not, on its own, a failure.

Every operation reports the following per component, in human-readable output: the component ID, release version, source asset, destination, action, managed status, and a reason. `--json` and `--toon` output are not part of this command yet.

The action is drawn from a shared set but is limited per command. `install` reports `created`, `updated`, `skipped`, or `failed`. `uninstall` reports `removed`, `not-installed`, `skipped`, or `failed`. `status` reports `installed`, `not-installed`, `skipped`, or `failed`.

### `git kura tools status [component...]`

Show the install state of tool components from local metadata, the filesystem, and git config only.

```sh
git kura tools status
git kura tools status pre-commit claude-skill
```

`status` never accesses the network and does not check whether the expected release asset is available remotely. With no component it reports every registered component; with one or more components it reports only those.

### `git kura tools install <component...>`

Install one or more tool components from the tools release asset that matches this binary's release version.

```sh
git kura tools install pre-commit
git kura tools install --all
```

`install` downloads the tools asset archive (`git-kura-tools_<version>.tar.gz`) and its sidecar manifest (`git-kura-tools_<version>.json`) for the same release tag as the binary; the `latest` release is never used. It verifies the archive checksum against the sidecar manifest before extracting the archive. Only the `sha256` checksum algorithm is supported; any other algorithm fails the install. A checksum mismatch is treated as a release asset inconsistency and aborts the install.

A verified archive is cached under `<git-common-dir>/kura/tools/cache/<version>/`. A cache is reused only when its recorded release version, archive name, archive checksum, and checksum algorithm all match the sidecar manifest. Because a release asset is immutable, a cache recorded for the same version that disagrees with the sidecar manifest on the archive checksum is treated as a release asset inconsistency: git-kura fails the install rather than using the cache or silently replacing it with the new bytes.

`install` requires an official release binary. A binary built with `go install` or from source does not have an injected release version, so it cannot download release assets and the install fails.

Installing a component already installed from the same asset is idempotent and reports `skipped`.

### `git kura tools uninstall <component...>`

Remove one or more installed tool components, using local metadata to decide what is safe to remove.

```sh
git kura tools uninstall pre-commit
git kura tools uninstall --all
```

A component with no metadata is reported `not-installed` and nothing changes. For a file-managed component, git-kura removes the destination only when the current file checksum matches the value recorded at install time; a file that no longer matches is treated as user-modified or externally modified and is left in place with a `skipped` result and a reason. An unmanaged file is never removed. For a config-managed component, git-kura reverts the value only when it still equals the value git-kura set; a changed value is left untouched and reported `skipped`. When metadata is corrupt, git-kura cannot decide what is safe and reports `failed` without changing anything.

### Tools install metadata

`install`, `uninstall`, and `status` share a metadata store at `<git-common-dir>/kura/tools/installed.json`, with a lock file at `<git-common-dir>/kura/tools/installed.lock`.

The store records, per installed component, a `schemaVersion`, the component ID, source asset id, release version, release asset name, destination path, installed version, checksum, managed mode (`file` or `config`), component-specific metadata, and `createdAt` / `updatedAt` timestamps.

`install` and `uninstall` take the metadata lock for the whole read-modify-write and write the store atomically. If the lock cannot be acquired within the retry timeout (see `kura.sealLockTimeoutMs`), the command fails with `seal-lock-timeout` (exit code 5) and changes no metadata, file, or config. If the store cannot be read, parsed, or validated against its schema — including an unsupported `schemaVersion` — git-kura refuses the destructive operation and fails without changing anything. `status` reads the store without taking the lock, so a held lock never blocks it.

## Configuration

Kura reads its settings from Git config, so they follow Git's standard scope resolution (local / global / system).

### `kura.sealLockTimeoutMs`

Maximum time, in milliseconds, that `seal claim`, `seal unclaim`, and `close` wait to acquire the seal store lock (`paths.lock`), and that `tools install` and `tools uninstall` wait to acquire the tools metadata lock (`installed.lock`), before failing with `seal-lock-timeout` (exit code 5).

```sh
git config kura.sealLockTimeoutMs 5000
```

The value is resolved through Git's standard config scopes; it is not restricted to `--local`. When unset, the timeout defaults to `5000` (5 seconds).

After stripping a trailing newline, the value must consist solely of decimal digits and is interpreted as a non-negative integer number of milliseconds. Values such as `+5`, ` 5`, `5 `, `5s`, `abc`, `-1`, the empty string, and decimals are rejected, and the command fails with an error that names the invalid value.

`0` is valid: the lock is acquired in a single attempt with no retry, so if the lock is already held the command fails immediately with `seal-lock-timeout`.

The timeout is capped at one hour (`3600000` ms). Values larger than that — including integers too large to represent — are rejected as errors.

`seal test` and `seal ls` never take the store lock, so they do not depend on this setting.

## Exit codes

Kura uses stable exit codes so scripts and AI-agent workflows can react correctly.

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage error |
| 3 | Unsafe operation refused |
| 4 | Not found |
| 5 | Seal lock timeout |
| 6 | Seal conflict |
| 7 | Seal doctor error |
| 8 | Repository context error |

Exit code 5 is signalled by `seal claim`, `seal unclaim`, `close`, `tools install`, and `tools uninstall`. Exit code 6 is signalled by `seal claim`, `seal unclaim`, and `seal test`. Exit code 7 is signalled by `seal doctor` when the seal store fails integrity validation. Exit code 8 is signalled by `tools` subcommands when they require a git repository context but are run outside one. The stderr message always starts with a stable reason token (`seal-lock-timeout:` or `tools-metadata-lock-timeout:` for code 5, `seal-conflict:`, `seal-doctor-error:`, or `not-in-git-repository:` for code 8) that scripts can match without parsing arbitrary text.
