package tag

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tooppoo/git-kura/scripts/release/internal/schema"
)

// Handler implements the tag release step: creating and pushing an annotated
// git tag for the target version.
type Handler struct{}

// New returns a Handler for the tag step.
func New() *Handler { return &Handler{} }

// BuildPayload returns a minimal step-specific JSON payload.
// The version is already recorded in the plan envelope; no additional
// fields are needed for the tag step.
func (h *Handler) BuildPayload(_ string) (json.RawMessage, error) {
	return json.Marshal(struct{}{})
}

// Validate checks all preflight conditions and returns human-readable errors
// and warnings. A non-nil third return value indicates an unexpected internal
// failure (e.g. git not found), distinct from a failed validation condition.
func (h *Handler) Validate(plan *schema.ReleasePlanEnvelope) ([]string, []string, error) {
	return runPreflight(plan.Payload.TargetVersion)
}

// Preflight re-runs the same checks as Validate immediately before Exec.
// A non-nil return prevents Exec from running.
func (h *Handler) Preflight(plan *schema.ReleasePlanEnvelope) error {
	errs, _, err := runPreflight(plan.Payload.TargetVersion)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf("preflight failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// Exec creates an annotated tag and pushes it to origin.
// It is only called after Preflight succeeds.
func (h *Handler) Exec(plan *schema.ReleasePlanEnvelope) error {
	version := plan.Payload.TargetVersion

	fmt.Printf("creating annotated tag %s...\n", version)
	if err := runGitVisible("tag", "-a", version, "-m", version); err != nil {
		return fmt.Errorf("create tag %q: %w", version, err)
	}
	fmt.Printf("tag %s created\n", version)

	fmt.Printf("pushing tag %s to origin...\n", version)
	if err := runGitVisible("push", "origin", version); err != nil {
		fmt.Fprintf(os.Stderr, "\nIMPORTANT: local tag %q may already exist.\n"+
			"Do NOT re-run exec without first checking the repository state.\n\n"+
			"Recovery steps:\n"+
			"  1. Check remote tag state:  git ls-remote origin refs/tags/%s\n"+
			"  2. If the remote tag does not exist and you want to retry:\n"+
			"       a. Delete the local tag:  git tag -d %s\n"+
			"       b. Re-run: plan → validate → exec\n"+
			"  3. If the push was rejected because the tag already exists on the\n"+
			"     remote, do NOT force-push — investigate the remote tag first\n",
			version, version, version,
		)
		return fmt.Errorf("push tag %q: %w", version, err)
	}
	fmt.Printf("tag %s pushed to origin successfully\n", version)
	return nil
}

// runPreflight executes all pre-exec safety checks and returns
// (validationErrors, warnings, internalError).
func runPreflight(version string) ([]string, []string, error) {
	var errs []string

	clean, details, err := isWorkingTreeClean()
	if err != nil {
		return nil, nil, fmt.Errorf("check working tree: %w", err)
	}
	if !clean {
		errs = append(errs, fmt.Sprintf("working tree is not clean:\n%s", details))
	}

	branch, err := currentBranch()
	if err != nil {
		return nil, nil, fmt.Errorf("check current branch: %w", err)
	}
	if branch != "main" {
		errs = append(errs, fmt.Sprintf("current branch is %q, must be main", branch))
	}

	headCommit, err := gitRevParse("HEAD")
	if err != nil {
		return nil, nil, fmt.Errorf("resolve HEAD: %w", err)
	}
	localMain, err := gitRevParse("refs/heads/main")
	if err != nil {
		return nil, nil, fmt.Errorf("resolve refs/heads/main: %w", err)
	}
	remoteMain, err := lsRemoteMain()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve remote refs/heads/main: %w", err)
	}
	if headCommit != localMain || headCommit != remoteMain {
		errs = append(errs, fmt.Sprintf(
			"commit ID mismatch — HEAD=%s, local main=%s, remote main=%s;\n"+
				"    ensure the branch is up-to-date with the remote before tagging",
			headCommit, localMain, remoteMain,
		))
	}

	localExists, err := localTagExists(version)
	if err != nil {
		return nil, nil, fmt.Errorf("check local tag %q: %w", version, err)
	}
	if localExists {
		errs = append(errs, fmt.Sprintf("local tag %q already exists", version))
	}

	remoteExists, err := remoteTagExists(version)
	if err != nil {
		return nil, nil, fmt.Errorf("check remote tag %q: %w", version, err)
	}
	if remoteExists {
		errs = append(errs, fmt.Sprintf("remote tag %q already exists", version))
	}

	return errs, nil, nil
}

// isWorkingTreeClean returns (true, "", nil) when no tracked changes or
// untracked files are present. Ignored files are not reported.
func isWorkingTreeClean() (bool, string, error) {
	// --porcelain omits ignored files by default (they need --ignored to appear).
	out, err := gitCapture("status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return true, "", nil
	}
	return false, out, nil
}

// currentBranch returns the name of the current branch.
func currentBranch() (string, error) {
	out, err := gitCapture("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// gitRevParse returns the full commit SHA for ref.
func gitRevParse(ref string) (string, error) {
	out, err := gitCapture("rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// lsRemoteMain queries the remote directly for refs/heads/main.
// It does NOT rely on refs/remotes/origin/main so a stale remote-tracking ref
// cannot produce a false positive.
func lsRemoteMain() (string, error) {
	out, err := gitCapture("ls-remote", "origin", "refs/heads/main")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] == "refs/heads/main" {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("refs/heads/main not found on remote origin")
}

// localTagExists reports whether refs/tags/<version> exists in the local repo.
func localTagExists(version string) (bool, error) {
	out, err := gitCapture("tag", "-l", version)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// remoteTagExists queries the remote directly for refs/tags/<version>.
// It matches only the exact ref name so peeled annotated-tag refs
// (refs/tags/<version>^{}) do not cause a false positive.
func remoteTagExists(version string) (bool, error) {
	want := "refs/tags/" + version
	out, err := gitCapture("ls-remote", "origin", want)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] == want {
			return true, nil
		}
	}
	return false, nil
}

// gitCapture runs git with args and returns stdout as a string.
func gitCapture(args ...string) (string, error) {
	c := exec.Command("git", args...)
	out, err := c.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s",
				strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// runGitVisible runs git with args and lets stdout/stderr flow through to the
// terminal so the user can see progress (e.g. push output).
func runGitVisible(args ...string) error {
	c := exec.Command("git", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
