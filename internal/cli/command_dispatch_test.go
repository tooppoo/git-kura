package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// TestRunEntrypoint exercises the exported Run() function, covering its error
// dispatch branches (renderedError, exitError, plain error, success).
func TestRunEntrypoint(t *testing.T) {
	t.Run("success returns exitSuccess", func(t *testing.T) {
		var buf bytes.Buffer
		code := Run([]string{"-h"}, &buf, io.Discard, testVersion)
		if code != exitSuccess {
			t.Fatalf("Run(-h) = %d, want exitSuccess", code)
		}
		if !strings.Contains(buf.String(), "Usage: git kura") {
			t.Fatalf("Run(-h) stdout = %q, want usage", buf.String())
		}
	})

	t.Run("exitError returns its code", func(t *testing.T) {
		// Unknown command is a usage error (exit 2), exercising the exitError path in Run.
		code := Run([]string{"unknown"}, io.Discard, io.Discard, testVersion)
		if code != exitUsageError {
			t.Fatalf("Run(unknown) = %d, want exitUsageError", code)
		}
	})

	t.Run("usage error returns exitUsageError", func(t *testing.T) {
		code := Run([]string{"seal", "doctor", "--fix"}, io.Discard, io.Discard, testVersion)
		if code != exitUsageError {
			t.Fatalf("Run(seal doctor --fix) = %d, want exitUsageError", code)
		}
	})

	t.Run("renderedError returns its code without double printing", func(t *testing.T) {
		// seal ls --json outside a git repo triggers emitError → renderedError,
		// exercising the Run() branch that returns ExitCode(re.code) silently.
		dir := t.TempDir()
		var code ExitCode
		withWorkingDir(t, dir, func() {
			code = Run([]string{"seal", "ls", "--json"}, io.Discard, io.Discard, testVersion)
		})
		if code == exitSuccess {
			t.Fatalf("Run(seal ls --json outside repo) = exitSuccess, want error code")
		}
	})

	t.Run("plain stdout error returns exitGeneralError", func(t *testing.T) {
		// A broken stdout writer causes the help-print to return a plain (non-exitError)
		// error, exercising the Run() fallthrough to exitGeneralError.
		code := Run([]string{"-h"}, &brokenWriter{}, io.Discard, testVersion)
		if code != exitGeneralError {
			t.Fatalf("Run(-h with broken stdout) = %d, want exitGeneralError", code)
		}
	})
}

// brokenWriter always returns an error on Write, used to simulate stdout failures.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("write failed") }

func TestRunHelpAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "top-level short help", args: []string{"-h"}, want: "Usage: git kura"},
		{name: "top-level long help", args: []string{"--help"}, want: "Usage: git kura"},
		{name: "short version", args: []string{"-v"}, want: testVersion},
		{name: "long version", args: []string{"--version"}, want: testVersion},
		{name: "get help", args: []string{"get", "--help"}, want: "Usage: git kura get"},
		{name: "open help", args: []string{"open", "--help"}, want: "Usage: git kura open"},
		{name: "close help", args: []string{"close", "--help"}, want: "Usage: git kura close"},
		{name: "ls help", args: []string{"ls", "--help"}, want: "Usage: git kura ls"},
		{name: "seal help (short)", args: []string{"seal", "--help"}, want: "Usage: git kura seal"},
		{name: "seal ls help", args: []string{"seal", "ls", "--help"}, want: "Usage: git kura seal ls"},
		{name: "seal doctor help", args: []string{"seal", "doctor", "--help"}, want: "Usage: git kura seal doctor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := captureOutput(t, func(r *runner) error {
				return r.run(tc.args)
			})
			if err != nil {
				t.Fatalf("run(%v) error = %v, want nil", tc.args, err)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout, tc.want)
			}
		})
	}

	for _, args := range [][]string{
		{},
		{"unknown"},
	} {
		t.Run(strings.Join(append([]string{"error"}, args...), " "), func(t *testing.T) {
			if err := newTestRunner().run(args); err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
		})
	}
}

func TestRunArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"get"},
		{"get", "51", "--format"},
		{"open", "51", "--extra"},
		{"close", "51", "--extra"},
		{"ls", "unexpected"},
		{"seal"},
		{"seal", "unknown"},
		{"seal", "ls", "key1", "key2"},
		{"seal", "ls", "--all"},
		{"seal", "ls", "--key", "key1"},
		{"seal", "ls", "..invalid"},
		{"seal", "doctor", "key1"},
		{"seal", "doctor", "--fix"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if err := newTestRunner().run(args); err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
		})
	}
}

func TestRunSealDoctorUsageErrorsUseExitCode2(t *testing.T) {
	for _, args := range [][]string{
		{"seal", "doctor", "key1"},
		{"seal", "doctor", "--fix"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := newTestRunner().run(args)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != int(exitUsageError) {
				t.Fatalf("run(%v) exit code = %v, want %d (err: %v)", args, xe, exitUsageError, err)
			}
		})
	}
}

// TestCobraBuiltinCommandsDisabled guards that Cobra's built-in shell-completion
// and help subcommands are not exposed as user-facing features (issue #78 scope).
// Both should behave like any other unknown command (non-zero exit).
func TestCobraBuiltinCommandsDisabled(t *testing.T) {
	for _, args := range [][]string{
		{"completion"},
		{"completion", "bash"},
		{"completion", "zsh"},
		{"help"},
		{"help", "get"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code := Run(args, io.Discard, io.Discard, testVersion)
			if code == exitSuccess {
				t.Fatalf("Run(%v) = exitSuccess, want non-zero (cobra built-in must not be exposed)", args)
			}
		})
	}
}

// TestUsageErrorsExitCode2 guards commands whose argument-error paths were
// accidentally returning exit 1 instead of exit 2.
func TestUsageErrorsExitCode2(t *testing.T) {
	cases := [][]string{
		{},                   // no command
		{"frobnicate"},       // unknown top-level command
		{"completion"},       // cobra default completion must not be reachable
		{"help"},             // cobra default help subcommand must not be reachable
		{"close"},            // missing key
		{"ls", "unexpected"}, // unexpected positional
		{"seal"},             // seal with no subcommand
		{"seal", "bogus"},    // unknown seal subcommand
		{"seal", "test"},     // missing paths
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := newTestRunner().run(args)
			if err == nil {
				t.Fatalf("run(%v) = nil, want usage error", args)
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != int(exitUsageError) {
				t.Fatalf("run(%v) exit code = %d, want %d (exitUsageError) err: %v",
					args, xe.code, exitUsageError, err)
			}
		})
	}
}
