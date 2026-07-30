package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/tooppoo/git-kura/internal/dashboard"
	"github.com/tooppoo/git-kura/internal/output"
)

// ExitCode is the process exit code type for all git kura commands.
// Keep the numeric values in sync with the table in docs/commands.md.
type ExitCode int

const (
	exitSuccess         ExitCode = 0
	exitGeneralError    ExitCode = 1
	exitUsageError      ExitCode = 2
	exitUnsafeRefused   ExitCode = 3
	exitNotFound        ExitCode = 4
	exitSealLockTimeout ExitCode = 5
	exitSealConflict    ExitCode = 6
	exitSealDoctorError ExitCode = 7
)

// Run is the primary testable CLI entrypoint. It accepts raw args and
// io.Writer targets for stdout/stderr, runs the command, and returns the
// process exit code as an ExitCode. version is the binary version string
// (populated by the build system in cmd/git-kura/main.go).
func Run(args []string, stdout, stderr io.Writer, version string) ExitCode {
	r := &runner{stdout: stdout, stderr: stderr, version: version}
	err := r.run(args)
	if err == nil {
		return exitSuccess
	}
	if re, ok := errors.AsType[*renderedError](err); ok {
		return ExitCode(re.code)
	}
	_, _ = fmt.Fprintln(stderr, err)
	if xe, ok := errors.AsType[*exitError](err); ok {
		return ExitCode(xe.code)
	}
	return exitGeneralError
}

// runner holds the output writers and version string threaded through all
// command functions so they never reference os.Stdout/os.Stderr directly.
type runner struct {
	stdout  io.Writer
	stderr  io.Writer
	version string

	// dashboardInteractive and dashboardRun are seams for dashboard tests;
	// nil selects the real terminal-backed implementations in cmdDashboard.
	dashboardInteractive func() bool
	dashboardRun         func(loader func() (dashboard.Snapshot, error)) error
}

// exitError carries a specific exit code to be used by Run.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// renderedError is the exit-code-only sentinel a structured-output command
// returns after a renderer has already written its output (the JSON error
// envelope or the human stderr message). Run exits with the carried code
// without printing anything further, so it never appends a second stderr line
// after a JSON error envelope.
type renderedError struct{ code int }

func (e *renderedError) Error() string { return "" }

// exitCodeError wraps err with a specific exit code. Returns nil when err is nil.
func exitCodeError(code ExitCode, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: int(code), err: err}
}

// emitResult renders a successful Result through the selected renderer, writing
// to r.stdout/r.stderr.
func (r *runner) emitResult(mode output.RenderMode, res output.Result) error {
	return output.SelectRenderer(mode).RenderResult(r.stdout, r.stderr, res)
}

// emitError renders a CommandError through the selected renderer and returns the
// exit-code-only sentinel, so Run exits with the command's code without
// printing the failure again.
func (r *runner) emitError(mode output.RenderMode, cerr *output.CommandError) error {
	if err := output.SelectRenderer(mode).RenderError(r.stdout, r.stderr, cerr); err != nil {
		return err
	}
	return &renderedError{code: cerr.ExitCode}
}

// toCommandError converts a plain command failure into the framework's
// CommandError, preserving the existing exit-code contract.
func toCommandError(cmd output.Command, err error) *output.CommandError {
	code := "general-error"
	exitCode := int(exitGeneralError)
	var xe *exitError
	if errors.As(err, &xe) {
		exitCode = xe.code
		code = reasonForExitCode(xe.code)
	}
	return &output.CommandError{
		Command:  cmd,
		Code:     code,
		Message:  err.Error(),
		ExitCode: exitCode,
	}
}

// reasonForExitCode maps an exit code to the hyphen-case reason token.
func reasonForExitCode(code int) string {
	switch ExitCode(code) {
	case exitUsageError:
		return "usage-error"
	case exitUnsafeRefused:
		return "unsafe-refused"
	case exitNotFound:
		return "not-found"
	case exitSealLockTimeout:
		return "seal-lock-timeout"
	case exitSealConflict:
		return "seal-conflict"
	case exitSealDoctorError:
		return "seal-doctor-error"
	default:
		return "general-error"
	}
}

// mustCompileSchema compiles a JSON schema from raw bytes and panics on error.
func mustCompileSchema(name string, data []byte) *jsonschema.Schema {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("parse %s: %v", name, err))
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		panic(fmt.Sprintf("add %s resource: %v", name, err))
	}
	sch, err := c.Compile(name)
	if err != nil {
		panic(fmt.Sprintf("compile %s: %v", name, err))
	}
	return sch
}

// validateData marshals data and validates it against schema before it is
// wrapped in an envelope. A failure is an internal contract violation.
func validateData(schema *jsonschema.Schema, data any) error {
	out, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("internal: marshal data: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(out))
	if err != nil {
		return fmt.Errorf("internal: parse data: %w", err)
	}
	if err := schema.Validate(inst); err != nil {
		return fmt.Errorf("internal: json output does not conform to schema: %w", err)
	}
	return nil
}

var validKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateKey(key string) error {
	if !validKeyRe.MatchString(key) {
		return fmt.Errorf("invalid key %q: key must match [A-Za-z0-9][A-Za-z0-9._-]{0,127}", key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("invalid key %q: key must not contain \"..\"", key)
	}
	if strings.HasSuffix(key, ".") {
		return fmt.Errorf("invalid key %q: key must not end with \".\"", key)
	}
	if strings.HasSuffix(key, ".lock") {
		return fmt.Errorf("invalid key %q: key must not end with \".lock\"", key)
	}
	return nil
}
