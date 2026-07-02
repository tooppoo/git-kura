package cmd

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
)

var versionRe = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

func validateVersion(version string) error {
	if !versionRe.MatchString(version) {
		return fmt.Errorf("invalid version %q: must be in vMAJOR.MINOR.PATCH format", version)
	}
	return nil
}

func parseStep(registry *step.Registry, name string) (step.Step, step.Handler, error) {
	s, err := step.Parse(name)
	if err != nil {
		return "", nil, err
	}
	h, err := registry.Get(s)
	if err != nil {
		return "", nil, err
	}
	return s, h, nil
}

// planFilePath returns the path where the plan file is stored.
// Path: .git-kura/release/<version>/<step>/plan.json
func planFilePath(version, stepName string) string {
	return filepath.Join(".git-kura", "release", version, stepName, "plan.json")
}

// validateResultPath returns the path where the validate result is stored.
// Path: .git-kura/release/<version>/<step>/validate-result.json
func validateResultPath(version, stepName string) string {
	return filepath.Join(".git-kura", "release", version, stepName, "validate-result.json")
}

func newPlanID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate plan ID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// computePayloadHash returns "sha256:<hex>" from the compact JSON encoding of v.
func computePayloadHash(v any) (string, error) {
	compact, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal payload for hash: %w", err)
	}
	sum := sha256.Sum256(compact)
	return fmt.Sprintf("sha256:%x", sum), nil
}

func nowISO8601() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func stepFlagUsage() string {
	return "Release step name (one of: " + strings.Join(step.KnownNames(), ", ") + ")"
}

// NewRootCommand returns the cobra root command wired with plan/validate/exec.
func NewRootCommand(registry *step.Registry) *cobra.Command {
	root := &cobra.Command{
		Use:           "release",
		Short:         "Release operation support for git-kura",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(
		newPlanCommand(registry),
		newValidateCommand(registry),
		newExecCommand(registry),
	)
	return root
}
