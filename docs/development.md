# Development

This document describes how to build and test git-kura locally.

## Build

Build the `git-kura` binary into `./bin`:

```sh
make build
```

## Unit and integration tests

Run the Go unit and integration tests:

```sh
make test
```

Run the full quality gate used in CI (format check, vet, coverage, and vulnerability scan):

```sh
make check
```

## Walkthrough test

The walkthrough test is a POSIX `sh` script at [`scripts/walkthrough.sh`](../scripts/walkthrough.sh) that drives the real `git kura` command through representative end-to-end scenarios against a throwaway git repository created in a temporary directory.
It favours coverage of the common path over exhaustive variation, and it is independent of the Go unit and integration tests.
In CI it runs as a separate `walkthrough` job after `make build`.

The script covers opening multiple worktrees, claiming and unclaiming seals across worktrees (including the conflict cases), `seal ls`, `seal test`, `seal doctor` on a healthy and a corrupted store, and the `close` happy path (including that `close` releases the closed worktree's seals).
Commands that should succeed are asserted to exit 0, and commands that should fail are asserted to exit non-zero; conflict cases also assert that the stable reason token appears on stderr.

Run it with:

```sh
make walkthrough
```

`make walkthrough` builds the binary, prepends `./bin` to `PATH`, and runs the script so every command is invoked in the `git kura ...` form.
To run the script directly, make sure `git kura` resolves to the build you want to test, then:

```sh
PATH="$PWD/bin:$PATH" sh scripts/walkthrough.sh
```

The script creates and cleans up its own temporary git repository on exit, whether it passes or fails, and never modifies your development repository or user environment.
Windows is out of scope; the script targets Unix-like systems and avoids bash-specific syntax.
