---
name: code-fix
description: Ensure Go implementation changes are verified with make check, fix failures until success, and handle coverage failures by adding meaningful tests or using narrow, explicit Go coverage exclusions.
---

# go-make-check-verification

## Purpose

Ensure that every Go implementation change is verified by the repository's standard check command, and that coverage failures are resolved by improving tests unless there is a justified reason to exclude verification-only code from coverage.

## When to use this skill

Use this skill whenever you modify Go implementation code, Go tests, build configuration, schemas, fixtures, generated code configuration, or coverage-related configuration in this repository.

## Required verification loop

After changing implementation or test code, always run:

```sh id="cr99o0"
make check
```

If `make check` fails:

1. Read the error message carefully.
2. Identify the concrete failing category, such as:

   * formatting failure
   * lint failure
   * compile failure
   * unit test failure
   * integration test failure
   * schema or fixture validation failure
   * coverage failure
3. Fix the cause indicated by the error.
4. Run `make check` again.
5. Continue this loop until `make check` succeeds.

Do not treat the task as complete while `make check` is still failing.

If `make check` cannot be executed because of an environment limitation, missing tool, missing permission, network restriction, or external service failure, stop and report:

* the exact command attempted
* the exact error output
* whether the failure is caused by the repository code or by the execution environment
* the remaining unverified risk

## Go coverage policy

If coverage is low or a coverage threshold fails, first assume the correct response is to add or improve tests.

Prefer tests that cover observable behavior rather than tests that merely execute lines for the sake of increasing coverage.

When adding tests, prioritize:

1. public behavior and CLI-visible behavior
2. parser / evaluator / reporter boundaries
3. error cases and validation failures
4. regression cases related to the change being made
5. edge cases that are likely to break silently
6. package boundary behavior, especially when interfaces or adapters are involved

Do not lower the coverage threshold merely to make the check pass.

## Go coverage exclusion policy

Coverage exclusion is allowed only when the uncovered code is primarily verification-only, scaffolding-only, generated, or structurally unsuitable for meaningful behavioral testing.

Examples that may be eligible:

* builtin module implementations used mainly as validation scaffolding
* generated code
* test-only adapters
* fixture-only helpers
* code that exists only to support schema or artifact validation
* unreachable defensive branches that cannot be exercised without corrupting invariants

Before excluding code from coverage, confirm that adding a meaningful test would not be the better fix.

When excluding code, keep the exclusion as narrow as possible.

## Preferred Go exclusion approaches

Go does not provide a stable, official function-level `coverage off` attribute equivalent to Rust's cargo-llvm-cov cfg-gated `coverage(off)` usage.

Do not invent source comments such as:

```go id="s33s5w"
// coverage:ignore
```

or:

```go id="kk7sq2"
//go:coverage off
```

unless the repository already has an explicit, documented tool that consumes those comments.

Prefer the following approaches, in this order.

### 1. Move test-only helpers into `_test.go`

If the code exists only for tests, fixtures, or test scaffolding, move it into a `_test.go` file when possible.

Example:

```go id="lsczgq"
package parser

func newFixtureParser() *Parser {
	return &Parser{
		// test-only setup
	}
}
```

This is preferable when the code is not needed by production builds.

### 2. Isolate verification-only code into a separate package or directory

If a builtin, fixture, generator, or validation helper is not part of production behavior, isolate it so the coverage command can exclude it explicitly.

Example directory layout:

```text id="k37v0k"
internal/
  builtin/
    production_builtin.go
  verificationbuiltin/
    fixture_builtin.go
```

Then configure the repository's coverage command to exclude the verification-only package or directory.

### 3. Use build tags only when the file truly belongs to a separate build mode

Use `//go:build` only when the file should be included or excluded based on a real build mode.

Example:

```go id="lb5hcj"
//go:build verification

package verificationbuiltin
```

Do not use build tags merely to hide poorly tested production code from coverage.

If build tags are used for verification-only code, the Makefile must make the intended mode explicit.

Example:

```makefile id="ks27zx"
test:
	go test ./...

test-verification:
	go test -tags=verification ./...
```

### 4. Filter the coverage profile only as an explicit repository policy

If the repository needs to exclude generated or verification-only files from a coverage report, do it in the Makefile or a dedicated coverage script by filtering the coverage profile.

Example:

```makefile id="6dz9nn"
coverage.out:
	go test ./... -coverprofile=coverage.raw.out
	grep -vE '/internal/verificationbuiltin/|_generated\.go' coverage.raw.out > coverage.out
	go tool cover -func=coverage.out
```

The filtering pattern must be narrow and documented.

Do not silently filter broad paths such as all of `internal/` or all files containing `builtin` unless the repository has explicitly decided that those paths are outside the coverage target.

## Coverage exclusion documentation

Every coverage exclusion must include a short explanation in the relevant code, Makefile, or coverage script.

The explanation must state:

* what is being excluded
* why adding tests would not be meaningful
* why the exclusion is narrower than the alternative
* whether the excluded code is production code, generated code, fixture code, or verification-only code

Example:

```makefile id="tt43hm"
# Exclude internal/verificationbuiltin from coverage because it contains
# fixture-only builtin implementations used to validate the evaluator contract.
# Production builtin behavior remains covered under internal/builtin.
COVERAGE_EXCLUDE := /internal/verificationbuiltin/
```

## Completion criteria

The task is complete only when:

* the requested Go implementation change is made
* relevant tests are added or updated when behavior changes
* coverage failures are resolved by tests or justified narrow exclusions
* `make check` succeeds
* the final report states what was changed and that `make check` passed

Do not claim success unless `make check` was actually run successfully.
