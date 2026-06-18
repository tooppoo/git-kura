# git-kura

![GitHub Release](https://img.shields.io/github/v/release/tooppoo/git-kura)
[![CI](https://github.com/tooppoo/git-kura/actions/workflows/ci.yml/badge.svg)](https://github.com/tooppoo/git-kura/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/tooppoo/git-kura/graph/badge.svg?token=5f8XJ77qiN)](https://codecov.io/gh/tooppoo/git-kura)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

`git-kura` is a keyed worktree resolver for Git.

It maps issue, ticket, task or feature keys to deterministic Git worktrees for humans, AI coding agents, and reviewers.

![A screenshot showing when `git-kura` detects a conflict and the agent stops running](./docs/assets/image.png)

## Walkthrough

```sh
git kura open fizz-feature             # create a worktree and a branch in a repository
cd $(git kura get fizz-feature)        # move to the worktree

# edit, save and commit in the worktree

cd $(git kura get fizz-feature --root) # move to the repository root

git merge fizz-feature                 # merge changes to main stream

git kura close fizz-feature            # clean up worktree and branch
```

See [docs/commands.md](docs/commands.md) for the full command reference.

## Quick Start

```sh
curl -fsSL https://raw.githubusercontent.com/tooppoo/git-kura/main/install.sh | sh
```

Installs `git-kura` into `~/.local/bin`. See [docs/installation.md](docs/installation.md) for version pinning, custom install directories, checksum verification, and other installation methods.

## Why Kura?

When using Git worktrees with multiple AI coding agents, it is easy to lose track of which worktree belongs to which task.

For example, one agent may implement a change in a task-specific worktree while another tool or reviewer needs to inspect that exact worktree later. If the review target is selected manually, the workflow becomes fragile.

Kura solves this by making the task key the source of truth.

```txt
task key
  -> branch name
  -> worktree path
```

The same key should always resolve to the same workspace.

## Concept

Kura（`蔵`） is built around four ideas:

* **Keyed workspaces**: any stable key — an issue number, ticket ID, task name, feature slug, or review target — can identify one workspace.
* **Worktree isolation**: each key gets its own Git worktree and branch.
* **A small local kura**: Kura（`蔵`） keeps keyed worktrees and metadata in a repository-local store, so the right workspace can be put away, found, and cleaned up without relying on memory.
* **Deterministic resolution**: humans, scripts, AI coding agents, and reviewers can resolve the same key to the same workspace without guessing.

Kura is intentionally small. It is not a Git client, an AI agent manager, a pull request tool, an issue tracker client, or a project management tool. Its job is to provide a stable mapping between a key and a local Git worktree.

## Usage

See [docs/commands.md](docs/commands.md) for the command reference and [docs/output-format.md](docs/output-format.md) for structured output formats.

## Installation

See [docs/installation.md](docs/installation.md) for options such as `--version`, `--install-dir`, and `--require-signature`.

## Documentation

* [Installation](docs/installation.md)
* [Commands](docs/commands.md)
* [Output formats](docs/output-format.md)
* [State management](docs/state-management.md)
* [Design](docs/design.md)
* [Architecture decision records](docs/adr/)

## License

Apache License 2.0
