# Use bubbletea as the TUI runtime dependency for the dashboard

- Status: Accepted
- Created: 2026-07-30T11:26:12Z

## Context

[20260730T112611Z_add-read-only-tui-dashboard-for-seal-ownership.md](20260730T112611Z_add-read-only-tui-dashboard-for-seal-ownership.md) decides that git-kura provides `git kura dashboard`, a read-only TUI. Until now git-kura had no terminal UI code and only three direct runtime dependencies (cobra, jsonschema, toon-go), so adding a TUI library is a runtime dependency policy change that requires an ADR.

The dashboard needs raw-mode input, an alternate screen, resize handling, guaranteed terminal restore on quit, `Ctrl-C`, and error paths, and it must work on macOS, Linux, and Windows. It also must keep update and render logic testable without a terminal, and the repository enforces a 90% total coverage gate, so the untestable terminal-touching surface has to stay minimal.

## Decision

- git-kura must use `github.com/charmbracelet/bubbletea` (v1, MIT license) as the TUI runtime for `git kura dashboard`.
- `golang.org/x/term` must be used for the interactive-terminal check on stdin and stdout before the TUI starts.
- `github.com/creack/pty` may be used as a test-only dependency for Unix pseudo-terminal integration tests.
- The bubbletea `Model` (state, `Update`, `View`) and all snapshot, aggregation, filter, and sort logic must live in `internal/dashboard` and must not perform terminal I/O, so they are unit-testable without a terminal.
- Only the thin program-start function may touch the real terminal; it must delegate terminal restore on all exit paths to bubbletea.

## Alternatives Considered

### Hand-rolled TUI on golang.org/x/term and raw ANSI escapes

Smallest dependency footprint, but reliable Windows console input, resize handling, and restore-on-panic would have to be implemented and maintained in git-kura. That code is exactly the hard-to-test kind the coverage gate penalizes, and terminal correctness bugs would become git-kura bugs.

### tcell (github.com/gdamore/tcell)

A solid cross-platform terminal library, but it exposes an imperative event loop and cell buffer. Update logic tends to interleave with drawing, which conflicts with the requirement to keep interaction logic terminal-independent for tests. Its dependency footprint is comparable to bubbletea's.

### tview (github.com/rivo/tview)

Widget framework on top of tcell. The dashboard needs one custom list view, so the widget layer adds surface without removing meaningful work, and the same testability concern as tcell applies.

## Consequences

### Positive Consequences

- The Elm-style model keeps `Update` and `View` pure, so the whole interaction cycle including error, stale, filter, and resize states is covered by ordinary unit tests.
- Raw mode, alternate screen, Windows console handling, resize events, and terminal restore on quit, `Ctrl-C`, and panic are handled by a widely used library instead of project-owned code.
- All added libraries are MIT licensed and pass the existing `go-licenses` allowlist check.

### Negative Consequences

- The dependency tree grows by roughly a dozen transitive modules (termenv, lipgloss, ansi handling, and others), enlarging `go.sum`, `third_party_licenses`, and the vulnerability-scan surface.
- bubbletea v1 is in maintenance while v2 changes its API; a future migration may require reworking the model plumbing.

### Neutral Consequences

- The dashboard renders with plain text and no styling library usage, so lipgloss remains an unused transitive dependency for now.
