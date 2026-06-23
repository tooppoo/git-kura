package main

import (
	"strings"
	"testing"

	"github.com/tooppoo/git-kura/internal/tools"
)

func TestToolsUsageErrors(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: mustProductionRegistry(t), version: fixtureVersion, fetcher: &fakeFetcher{}}

	cases := [][]string{
		{},                                   // no subcommand
		{"bogus"},                            // unknown subcommand
		{"install"},                          // install without component
		{"uninstall"},                        // uninstall without component
		{"install", "--all", "pre-commit"},   // --all + explicit
		{"uninstall", "--all", "pre-commit"}, // --all + explicit
		{"status", "--all"},                  // status does not accept --all
		{"install", "no-such-component"},     // unknown component
		{"status", "no-such-component"},      // unknown component
		{"install", "--bogus"},               // unknown flag
	}
	for _, args := range cases {
		_, err := runToolsCLI(t, repo, deps, args...)
		requireToolsExit(t, err, exitUsageError)
	}
}

func TestToolsHelp(t *testing.T) {
	deps := toolsDeps{registry: mustProductionRegistry(t)}
	for _, args := range [][]string{{"-h"}, {"--help"}, {"status", "-h"}, {"install", "--help"}, {"uninstall", "-h"}} {
		out, err := captureStdout(t, func() error { return runToolsWith(deps, args) })
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(out, "Usage") {
			t.Fatalf("%v: expected usage help, got %q", args, out)
		}
	}
	// The production entrypoint wires the real registry/fetcher; help needs no
	// repository or network.
	if _, err := captureStdout(t, func() error { return runTools([]string{"--help"}) }); err != nil {
		t.Fatalf("runTools --help: %v", err)
	}
}

func TestToolsFailsOutsideGitRepository(t *testing.T) {
	dir := t.TempDir() // not a git repository
	deps := toolsDeps{registry: mustProductionRegistry(t), version: fixtureVersion, fetcher: &fakeFetcher{}}
	var err error
	withWorkingDir(t, dir, func() {
		_, err = captureStdout(t, func() error { return runToolsWith(deps, []string{"status"}) })
	})
	if err == nil {
		t.Fatal("status outside a git repository should fail")
	}
}

func TestToolsStatusShowsAllComponentsWhenUnspecified(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: mustProductionRegistry(t), version: fixtureVersion, fetcher: &fakeFetcher{}}

	out, err := runToolsCLI(t, repo, deps, "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, id := range []string{"pre-commit", "claude-skill", "codex-skill"} {
		if !strings.Contains(out, id) {
			t.Fatalf("status output missing %q:\n%s", id, out)
		}
	}
	if !strings.Contains(out, "not-installed") {
		t.Fatalf("status output should report not-installed:\n%s", out)
	}
}

func TestToolsStatusSingleComponent(t *testing.T) {
	repo := toolsTestRepo(t)
	deps := toolsDeps{registry: mustProductionRegistry(t), version: fixtureVersion, fetcher: &fakeFetcher{}}

	out, err := runToolsCLI(t, repo, deps, "status", "pre-commit")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "pre-commit") {
		t.Fatalf("status output missing pre-commit:\n%s", out)
	}
	if strings.Contains(out, "claude-skill") {
		t.Fatalf("status of one component should not list others:\n%s", out)
	}
}

func TestToolsInstallAllTargetsEveryComponent(t *testing.T) {
	repo := toolsTestRepo(t)
	a := &fileFixture{componentID: "alpha", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/alpha.txt"}
	b := &fileFixture{componentID: "beta", archiveRel: "alpha/tool.txt", destRel: ".kura-tools/beta.txt"}
	content := []byte("shared\n")
	manifest := tools.ArchiveManifest{SchemaVersion: 1, Components: map[string]tools.ArchiveManifestComponent{
		"alpha": {Files: map[string]string{a.archiveRel: tools.SHA256Hex(content)}},
		"beta":  {Files: map[string]string{b.archiveRel: tools.SHA256Hex(content)}},
	}}
	archive := makeToolsArchive(t, manifest, map[string][]byte{a.archiveRel: content})
	sidecar := makeSidecar(t, fixtureVersion, fixtureArchiveName, tools.ChecksumAlgorithmSHA256, archive)
	deps := toolsDeps{registry: mustToolsRegistry(t, a, b), version: fixtureVersion, fetcher: &fakeFetcher{sidecar: sidecar, archive: archive}}

	out, err := runToolsCLI(t, repo, deps, "install", "--all")
	if err != nil {
		t.Fatalf("install --all: %v", err)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("install --all should target every component:\n%s", out)
	}
	assertPathExists(t, a.destAbs(repo))
	assertPathExists(t, b.destAbs(repo))
}

func TestToolsInvalidActionFromComponentBecomesFailure(t *testing.T) {
	repo := toolsTestRepo(t)
	// status may not emit "created"; a component that does is treated as a bug
	// and reported failed.
	comp := componentFunc{idVal: "weird", statusFn: func(tools.Context) tools.Outcome {
		return tools.Outcome{Result: tools.Result{Component: "weird", Action: tools.ActionCreated}}
	}}
	deps := toolsDeps{registry: mustToolsRegistry(t, comp), version: fixtureVersion, fetcher: &fakeFetcher{}}
	out, err := runToolsCLI(t, repo, deps, "status", "weird")
	requireToolsExit(t, err, exitGeneralError)
	if !strings.Contains(out, "failed") || !strings.Contains(strings.ToLower(out), "internal") {
		t.Fatalf("invalid action should become an internal failure:\n%s", out)
	}
}

func TestToolsRepositoryContextError(t *testing.T) {
	outside := t.TempDir()
	deps := toolsDeps{
		registry: mustToolsRegistry(t, tools.NewClaudeSkillComponent()),
		version:  fixtureVersion,
		fetcher:  &fakeFetcher{},
	}

	_, err := runToolsCLI(t, outside, deps, "status", tools.ClaudeSkillComponentID)
	requireToolsExit(t, err, exitRepositoryContextError)
}

func TestToolsInstallRepositoryContextError(t *testing.T) {
	outside := t.TempDir()
	content := []byte("skill")
	deps := skillDeps(t, content, content)

	_, err := runToolsCLI(t, outside, deps, "install", tools.ClaudeSkillComponentID)
	requireToolsExit(t, err, exitRepositoryContextError)
}

func TestToolsUninstallRepositoryContextError(t *testing.T) {
	outside := t.TempDir()
	content := []byte("skill")
	deps := skillDeps(t, content, content)

	_, err := runToolsCLI(t, outside, deps, "uninstall", tools.ClaudeSkillComponentID)
	requireToolsExit(t, err, exitRepositoryContextError)
}

func TestToolsRunDispatch(t *testing.T) {
	if err := runToolsRun([]string{"--help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	if got := exitCodeOf(runToolsRun(nil)); got != exitUsageError {
		t.Fatalf("no args should be usage error, got %d", got)
	}
	if got := exitCodeOf(runToolsRun([]string{"bogus"})); got != exitUsageError {
		t.Fatalf("unknown target should be usage error, got %d", got)
	}
}

func TestRunToolsWithRunSubcommand(t *testing.T) {
	repo := toolsTestRepo(t)
	stage(t, repo, "seed.txt", "seed\n")
	git(t, repo, "commit", "-m", "seed")
	stage(t, repo, "free.txt", "free\n")
	deps := preCommitDeps(t)
	var err error
	withWorkingDir(t, repo, func() {
		_, err = captureStdout(t, func() error { return runToolsWith(deps, []string{"run", "pre-commit"}) })
	})
	if err != nil {
		t.Fatalf("tools run pre-commit: %v", err)
	}
}
