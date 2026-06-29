# Exclude release scripts from the coverage gate

- Status: Accepted
- Created: 2026-06-29T08:54:14Z

## Context

git-kura enforces a 90% coverage threshold in CI. The coverage command previously used `-coverpkg=./...`, so every Go package in the repository contributed to the coverage denominator.

The release support code under `scripts/release/` is maintainer-facing release operation machinery. It is tested and vetted in CI, but it is not part of the public `git kura` CLI runtime behavior and is not distributed as a tool asset. This boundary is documented in [scripts/README.md](../../scripts/README.md) and [docs/internal/repository-layout.md](../internal/repository-layout.md).

As release automation grows, adding release-only orchestration code can move the repository-wide coverage percentage even when the distributed CLI behavior is unchanged. That makes the 90% coverage gate less useful as a signal for the testedness of the shipped CLI and its internal packages.

This decision needs an ADR because it changes the durable meaning of the CI coverage gate and creates an explicit coverage exclusion policy for a repository path.

## Decision

`scripts/release/...` must remain in the normal CI test and vet scope, but it must not be part of the 90% coverage gate denominator.

The `coverage` recipe must continue to run `go test ... ./...` so tests for `scripts/release/...` still execute. Only `-coverpkg` is narrowed: it is computed from `go list ./...` with packages under `/scripts/release` removed. The resulting `coverage.out` profile represents the distributed CLI and its internal packages, not maintainer-facing release scripts.

Release script quality should be protected with targeted unit tests and, where needed, smoke or integration checks for release workflows. Coverage percentage is not the selected gate for this directory.

## Alternatives Considered

### Keep `scripts/release/...` in the coverage denominator

This would make all release automation contribute to the single repository coverage percentage. It was not selected because release scripts are operational maintainer tooling rather than shipped CLI runtime behavior, and their growth can make the CLI coverage signal noisier without improving user-facing risk measurement.

### Lower the coverage threshold

Lowering the threshold would avoid immediate CI failures, but it would weaken the gate for the distributed CLI packages. It was rejected because the problem is the denominator, not the desired coverage standard for the shipped CLI behavior.

### Stop testing `scripts/release/...` in CI

This was rejected. Release scripts can affect project operations and must continue to compile and pass their targeted tests in CI.

## Consequences

### Positive Consequences

- The 90% coverage gate remains focused on the distributed CLI and its internal packages.
- Release scripts can grow without forcing line-coverage-driven tests for maintainer-only orchestration code.
- `scripts/release/...` tests still run in CI because the test package list remains `./...`.

### Negative Consequences

- Codecov and local `coverage.out` no longer report release script coverage as part of the main coverage profile.
- Maintainers must rely on targeted release script tests and smoke checks rather than the global coverage threshold for release automation.

### Neutral Consequences

- `go test ./...` remains the command for running all Go tests, including `scripts/release/...`.
- Future maintainer-facing directories are not automatically excluded; each additional exclusion needs its own explicit policy.
