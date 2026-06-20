package main

import (
	"errors"
	"strings"
	"testing"
)

func TestRunHelpAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "top-level short help", args: []string{"-h"}, want: "Usage: git kura"},
		{name: "top-level long help", args: []string{"--help"}, want: "Usage: git kura"},
		{name: "short version", args: []string{"-v"}, want: version},
		{name: "long version", args: []string{"--version"}, want: version},
		{name: "get help", args: []string{"get", "--help"}, want: "Usage: git kura get"},
		{name: "open help", args: []string{"open", "--help"}, want: "Usage: git kura open"},
		{name: "close help", args: []string{"close", "--help"}, want: "Usage: git kura close"},
		{name: "ls help", args: []string{"ls", "--help"}, want: "Usage: git kura ls"},
		{name: "seal help (short)", args: []string{"seal", "--help"}, want: "Usage: git kura seal"},
		{name: "seal ls help", args: []string{"seal", "ls", "--help"}, want: "Usage: git kura seal ls"},
		{name: "seal doctor help", args: []string{"seal", "doctor", "--help"}, want: "Usage: git kura seal doctor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := captureStdout(t, func() error {
				return run(tc.args)
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
			if err := run(args); err == nil {
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
			if err := run(args); err == nil {
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
			err := run(args)
			if err == nil {
				t.Fatalf("run(%v) error = nil, want error", args)
			}
			var xe *exitError
			if !errors.As(err, &xe) || xe.code != exitUsageError {
				t.Fatalf("run(%v) exit code = %v, want %d (err: %v)", args, xe, exitUsageError, err)
			}
		})
	}
}
