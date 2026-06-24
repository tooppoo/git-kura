# Technology Stack for Kura

- Status: Accepted
- Created: 2026-06-08T12:40:00Z

## Context

Kura is a keyed worktree resolver for Git.

Its core responsibility is intentionally narrow: given a stable key such as an issue, ticket, or task number, Kura creates, resolves, and removes a deterministic Git worktree.

Kura is designed around the following requirements:

* lightweight and fast execution
* cross-platform support
* no runtime dependency other than Git
* script-friendly output
* AI-friendly metadata output
* small implementation surface
* conservative behavior around destructive operations

Kura should not become a general-purpose Git client, AI agent manager, pull request orchestrator, or issue tracker client. Those tools may integrate with Kura, but Kura itself should remain focused on keyed worktree lifecycle resolution.

## Decision

Kura will be implemented in Go.

The distributed executable will be a single binary named `git-kura`, intended to be invoked as a Git subcommand:

```sh
git kura open 51
git kura get 51 --path
git kura get 51 --branch
git kura get 51 --json
git kura get 51 --toon
git kura close 51
```

Kura will use the Git command-line executable for Git operations. It will not depend on `libgit2`, GitHub CLI, GitLab CLI, Node.js, Python, or any other runtime command.

Runtime dependencies:

```txt
git
```

Build-time dependencies:

```txt
Go toolchain
Go modules
```

Go module dependencies are allowed, provided they are compiled into the distributed binary and do not introduce additional runtime requirements for users.

## Runtime Dependency Policy

The phrase “no dependency other than Git” means:

```txt
Kura users should not need to install any runtime dependency other than Git.
```

It does not mean:

```txt
Kura must have no Go module dependencies.
```

Therefore, Kura may use Go module dependencies where they reduce implementation risk or improve standards compliance. The resulting binary must remain self-contained from the user's perspective.

## Git Integration

Kura will invoke Git through the Git CLI.

Examples of Git commands Kura may use internally include:

```txt
git rev-parse --show-toplevel
git rev-parse --git-common-dir
git worktree add
git worktree list --porcelain -z
git worktree remove
git status --porcelain=v1 -z
git branch --delete
```

Kura must not invoke Git through shell interpolation.

Preferred pattern:

```go
exec.Command("git", "worktree", "list", "--porcelain", "-z")
```

Avoided pattern:

```go
exec.Command("sh", "-c", "git worktree list --porcelain -z")
```

This reduces quoting, escaping, and command injection risks, especially for keys, branch names, and file paths.

## Output Formats

Kura will support both scalar and structured output.

Scalar output:

```sh
git kura get 51 --path
git kura get 51 --branch
```

Structured output:

```sh
git kura get 51 --json
git kura get 51 --toon
```

JSON is the canonical machine-readable format.

[TOON](https://github.com/toon-format/toon) is an AI-oriented, prompt-friendly projection generated from the same metadata model as JSON.

```txt
JSON:
  canonical
  stable
  programmatic
  suitable for scripts and tools

TOON:
  prompt-friendly
  AI-oriented
  compact
  suitable for LLM prompts and agent context
```

Kura must not treat TOON as the only structured output format. JSON remains the compatibility contract for external tools.

## TOON Support

Kura may use the Go implementation of TOON, such as `github.com/toon-format/toon-go`, rather than implementing TOON formatting manually.

Because TOON is intended for AI-facing metadata and may evolve, Kura must protect its output behavior with golden tests.

Required test fixtures should include at least:

```txt
get_issue_51.json.golden
get_issue_51.toon.golden
```

The same internal metadata object must be used to generate both JSON and TOON output.

Example metadata model:

```go
type WorktreeMetadata struct {
    SchemaVersion  int    `json:"schemaVersion" toon:"schemaVersion"`
    Key            string `json:"key" toon:"key"`
    Kind           string `json:"kind" toon:"kind"`
    Branch         string `json:"branch" toon:"branch"`
    WorktreePath   string `json:"worktreePath" toon:"worktreePath"`
    RepositoryRoot string `json:"repositoryRoot" toon:"repositoryRoot"`
    BaseBranch     string `json:"baseBranch" toon:"baseBranch"`
    Exists         bool   `json:"exists" toon:"exists"`
    Dirty          bool   `json:"dirty" toon:"dirty"`
}
```

## CLI Parsing

Kura will open with a small hand-written CLI parser or Go standard-library based command dispatch.

External CLI frameworks such as Cobra or urfave/cli are not required for v0.

This is intentional. Kura has a narrow command surface:

```txt
open <key>
get <key>
end <key>
```

Adding a large CLI framework at v0 would increase dependency and abstraction cost without clearly improving the core design.

This decision may be revisited if Kura later gains a larger command surface.

## Platform Support

Kura should support at least:

```txt
macOS
Linux
Windows
```

Path handling must use platform-aware APIs such as Go's `path/filepath`.

Kura must distinguish between:

```txt
Git branch names
filesystem paths
```

For example:

```txt
branch: issue/51
path:   <platform-specific parent>/<repo>-issue-51
```

Branch naming may use `/`, but filesystem paths must be constructed using platform-aware path operations.

## Internal Architecture

Kura should remain small, but its implementation should separate the following concerns:

```txt
cmd/git-kura:
  process entrypoint

internal/cli:
  argument parsing
  stdout/stderr behavior
  exit codes

internal/git:
  Git command execution
  worktree parsing
  status parsing

internal/kura:
  key validation
  branch naming
  worktree path derivation
  metadata model
  JSON/TOON output
  safety policy
```

A possible initial layout:

```txt
git-kura/
  go.mod
  LICENSE
  README.md
  cmd/
    git-kura/
      main.go
  internal/
    cli/
      cli.go
    git/
      git.go
      worktree.go
      status.go
    kura/
      key.go
      naming.go
      metadata.go
      output.go
      safety.go
```

This is not intended as a heavy architecture. The purpose is only to prevent CLI parsing, Git process execution, naming rules, and safety policy from becoming tangled.

## Safety Policy

Kura must be conservative around destructive operations.

In particular, `git kura close <key>` must not silently discard work.

Before removing a worktree, Kura should check conditions such as:

* whether the worktree exists
* whether the worktree corresponds to the expected key
* whether the worktree has uncommitted changes
* whether the worktree has untracked files
* whether the branch/worktree mapping is consistent
* whether submodule state needs special handling

If the operation is unsafe, Kura should refuse and explain the reason.

Force options may be added later, but they must be explicit.

## Exit Codes

Kura should use stable exit codes so scripts and AI-agent workflows can react correctly.

Initial exit code policy:

```txt
0  success
1  general error
2  usage error
3  unsafe operation refused
4  not found
```

This may be expanded later, but v0 should avoid excessive error-code granularity.

## Alternatives Considered

### Rust

Rust is a strong candidate for lightweight, fast, cross-platform CLI tools.

It was not selected for v0 because Kura's core work is mostly:

* invoking Git safely
* deriving deterministic names
* parsing Git output
* printing JSON and TOON metadata
* refusing unsafe deletion

These requirements do not strongly require Rust's ownership model or advanced type-system benefits.

Rust would also likely encourage dependencies such as `clap` and `serde`, which are good tools but add more setup and implementation weight than necessary for v0.

Rust remains a reasonable future option if Kura later requires stricter compile-time modeling, more complex parsing, or stronger performance constraints.

### Zig

Zig has attractive properties for small binaries and cross-compilation.

It was not selected for v0 because the surrounding CLI, JSON, testing, packaging, and contributor ecosystem are less immediately convenient for this project than Go.

Kura should prioritize boring reliability over language novelty.

### Deno

Deno was considered because it can compile TypeScript or JavaScript programs into standalone executables.

This means Deno can satisfy Kura's runtime dependency requirement if the distributed artifact is a compiled binary. Users would not need to install the Deno runtime to run Kura.

Deno also provides a strong developer experience for TypeScript, built-in tooling, and a security model based on explicit permissions.

It was not selected for v0 for the following reasons:

* Kura does not need a JavaScript or TypeScript runtime model.
* Kura's core logic is small enough to implement directly in Go.
* The compiled executable embeds a larger runtime than Kura needs.
* Permission and packaging behavior add concepts that are not central to Kura.
* Go provides a more conventional path for small, cross-platform Git subcommands.

Deno remains a reasonable alternative if the project later prioritizes TypeScript implementation speed or integration with a TypeScript-based toolchain.

### Bun

Bun was considered because it can build standalone executables from JavaScript or TypeScript programs.

This means Bun can also satisfy Kura's runtime dependency requirement if Kura is distributed as a compiled binary rather than as a script requiring Bun at runtime.

Bun has attractive properties for CLI development, including fast execution, integrated tooling, and simple packaging for JavaScript and TypeScript projects.

It was not selected for v0 for the following reasons:

* Kura does not need Bun-specific runtime or bundler features.
* Kura's core behavior is mostly Git process execution, path derivation, metadata formatting, and safety checks.
* Go gives Kura a simpler and more established single-binary distribution model for this use case.
* Bun's strengths are more relevant to JavaScript/TypeScript applications than to a narrow Git worktree resolver.
* Depending on a rapidly evolving JavaScript runtime and bundler would add avoidable release and packaging risk.

Bun remains a reasonable alternative if Kura later needs closer integration with JavaScript-based agent tooling, but it is not the preferred implementation language for v0.

### TypeScript / Node.js

TypeScript on Node.js was considered because it offers fast development, a large ecosystem, and good ergonomics for command-line tools.

It was not selected because a plain Node.js implementation would require users to have Node.js installed at runtime, unless an additional packaging layer is introduced.

That conflicts with Kura's runtime dependency policy.

Deno and Bun reduce this concern through standalone executable support, but Kura's v0 requirements are still better served by Go.

### Shell Script

Shell would minimize build complexity on Unix-like systems.

It was not selected because Kura needs reliable cross-platform behavior, structured output, safe path handling, and Windows support. Shell would make those requirements fragile.

### libgit2

Using libgit2 would avoid shelling out to Git and could provide deeper Git integration.

It was not selected because Kura is intended to be a thin Git worktree workflow tool, not a Git implementation. Depending on the Git CLI keeps behavior aligned with the user's installed Git and avoids adding native library complexity.

### GitHub CLI

GitHub CLI could provide issue-related functionality.

It was not selected because Kura should not be GitHub-specific. The key may represent a GitHub issue, GitLab issue, Jira ticket, Linear task, or local task identifier. Kura should treat the key as a worktree-resolution key, not as an issue-tracker API object.

## Consequences

### Positive Consequences

* Kura can be distributed as a single binary.
* Users only need Git at runtime.
* Cross-platform builds are straightforward.
* The implementation remains small and approachable.
* JSON output provides a stable contract for tools and scripts.
* TOON output gives Kura a concrete AI-friendly interface.
* Git behavior remains aligned with the user's installed Git.
* Kura avoids becoming an AI session manager or full Git client.

### Negative Consequences

* Shelling out to Git requires careful process and error handling.
* Git output parsing must be tested across platforms and Git versions.
* Go may be less expressive than Rust for modeling domain invariants.
* TOON support introduces a build-time dependency and must be protected by golden tests.
* Hand-written CLI parsing may need revision if the command surface grows.

### Neutral Consequences

* Kura's behavior will depend on the installed Git version.
* Kura will not validate whether a key actually corresponds to a real issue in an issue tracker.
* Kura will not require GitHub CLI, even when the key happens to be a GitHub issue number.

## Non-Goals

Kura will not, in v0:

* manage AI coding sessions
* launch Claude Code, Codex, Gemini CLI, or other agents
* create or review pull requests
* call GitHub, GitLab, Jira, or Linear APIs
* provide a full Git TUI
* infer task identity from natural language
* store a database of task metadata as the source of truth
* replace Git worktree itself

Kura's source of truth is deterministic resolution from a key to a branch and worktree path.

## Summary

Kura will use Go for v0.

It will be distributed as a single `git-kura` binary, invoked as `git kura`.

It will depend on Git at runtime and may use Go module dependencies at build time.

It will use JSON as the canonical structured output format and TOON as an AI-friendly prompt-oriented format.

The guiding principle is:

```txt
Small tool, stable key, deterministic worktree.
```
