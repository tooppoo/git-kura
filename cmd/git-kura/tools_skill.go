package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

// exitRepositoryContextError is the exit code for commands that require a git
// repository context but were run outside one. It re-uses the number previously
// reserved for the guard command (removed in #68) so the exit-code registry
// stays compact.
const exitRepositoryContextError = 8

const (
	claudeSkillComponentID = "claude-skill"
	codexSkillComponentID  = "codex-skill"
)

// Archive-relative paths for each skill file inside the tools archive.
const (
	claudeSkillArchivePath = "claude-skill/SKILL.md"
	codexSkillArchivePath  = "codex-skill/SKILL.md"
)

func newClaudeSkillComponent() skillComponent {
	return skillComponent{
		componentID: claudeSkillComponentID,
		archivePath: claudeSkillArchivePath,
		destPath: func(representativeRoot string) string {
			return filepath.Join(representativeRoot, ".claude", "skills", "git-kura", "SKILL.md")
		},
	}
}

func newCodexSkillComponent() skillComponent {
	return skillComponent{
		componentID: codexSkillComponentID,
		archivePath: codexSkillArchivePath,
		destPath: func(representativeRoot string) string {
			return filepath.Join(representativeRoot, ".agents", "skills", "git-kura", "SKILL.md")
		},
	}
}

// skillComponent is the shared implementation for claude-skill and codex-skill.
// Each installs a single SKILL.md file from the tools archive into the
// representative root of the repository. The only difference between the two is
// the component ID, the archive source path, and the destination directory.
type skillComponent struct {
	componentID string
	archivePath string
	destPath    func(representativeRoot string) string
}

func (c skillComponent) id() string { return c.componentID }

func (c skillComponent) status(ctx toolsContext) toolsOutcome {
	repRoot, err := resolveRepresentativeRoot(ctx.repoRoot, ctx.commonDir)
	if err != nil {
		return toolsOutcome{result: toolsResult{
			Component: c.componentID,
			Action:    actionFailed,
			Reason:    err.Error(),
		}}
	}

	dest := c.destPath(repRoot)

	if ctx.entry == nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return toolsOutcome{result: toolsResult{
				Component:   c.componentID,
				Destination: dest,
				Action:      actionNotInstalled,
				Managed:     false,
				Reason:      "unmanaged-file-exists",
			}}
		}
		return toolsOutcome{result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionNotInstalled,
			Managed:     false,
			Reason:      "not-installed",
		}}
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return toolsOutcome{result: toolsResult{
				Component:      c.componentID,
				ReleaseVersion: ctx.entry.InstalledVersion,
				Destination:    dest,
				Action:         actionNotInstalled,
				Managed:        true,
				Reason:         "destination file missing",
			}}
		}
		return toolsOutcome{result: toolsResult{
			Component:      c.componentID,
			ReleaseVersion: ctx.entry.InstalledVersion,
			Destination:    dest,
			Action:         actionFailed,
			Reason:         fmt.Sprintf("read destination: %v", err),
		}}
	}

	if sha256hex(data) != ctx.entry.Checksum {
		return toolsOutcome{result: toolsResult{
			Component:      c.componentID,
			ReleaseVersion: ctx.entry.InstalledVersion,
			Destination:    dest,
			Action:         actionInstalled,
			Managed:        false,
			Reason:         "file was modified outside git-kura",
		}}
	}

	return toolsOutcome{result: toolsResult{
		Component:      c.componentID,
		ReleaseVersion: ctx.entry.InstalledVersion,
		Destination:    dest,
		Action:         actionInstalled,
		Managed:        true,
	}}
}

func (c skillComponent) install(ctx toolsInstallContext) toolsOutcome {
	repRoot, err := resolveRepresentativeRoot(ctx.repoRoot, ctx.commonDir)
	if err != nil {
		return toolsOutcome{result: toolsResult{
			Component:      c.componentID,
			ReleaseVersion: ctx.releaseVersion,
			Action:         actionFailed,
			Reason:         err.Error(),
		}}
	}

	dest := c.destPath(repRoot)

	fail := func(reason string) toolsOutcome {
		return toolsOutcome{result: toolsResult{
			Component:      c.componentID,
			ReleaseVersion: ctx.releaseVersion,
			Destination:    dest,
			Action:         actionFailed,
			Reason:         reason,
		}}
	}

	assetComp, ok := ctx.asset.componentAssets(c.componentID)
	if !ok {
		return fail(fmt.Sprintf("component %q not found in tools asset", c.componentID))
	}

	expectedSum, ok := assetComp.Files[c.archivePath]
	if !ok {
		return fail(fmt.Sprintf("archive path %q not in component manifest", c.archivePath))
	}

	data, err := os.ReadFile(ctx.asset.path(c.archivePath))
	if err != nil {
		return fail(fmt.Sprintf("read asset file: %v", err))
	}

	if sha256hex(data) != expectedSum {
		return fail("asset file checksum mismatch")
	}

	if ctx.entry == nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return fail("unmanaged-file-exists: destination file exists but is not managed by git-kura")
		}
	}

	if ctx.entry != nil {
		existing, readErr := os.ReadFile(dest)
		if readErr == nil {
			existingSum := sha256hex(existing)
			if existingSum != ctx.entry.Checksum {
				return fail("destination file was modified outside git-kura; not overwriting")
			}
			// File matches recorded checksum; if that also matches the new asset
			// there is nothing to do.
			if existingSum == expectedSum {
				return toolsOutcome{result: toolsResult{
					Component:      c.componentID,
					ReleaseVersion: ctx.releaseVersion,
					SourceAsset:    c.archivePath,
					Destination:    dest,
					Action:         actionSkipped,
					Managed:        true,
					Reason:         "already installed; checksum matches",
				}}
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fail(fmt.Sprintf("create destination dir: %v", err))
	}

	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fail(fmt.Sprintf("write destination: %v", err))
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fail(fmt.Sprintf("commit destination: %v", err))
	}

	action := actionCreated
	if ctx.entry != nil {
		action = actionUpdated
	}

	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	if ctx.entry != nil {
		created = ctx.entry.CreatedAt
	}

	entry := &toolsMetadataEntry{
		Component:        c.componentID,
		SourceAssetID:    c.archivePath,
		ReleaseVersion:   ctx.releaseVersion,
		InstalledVersion: ctx.releaseVersion,
		DestinationPath:  dest,
		Checksum:         expectedSum,
		ManagedMode:      managedModeFile,
		CreatedAt:        created,
		UpdatedAt:        now,
	}

	return toolsOutcome{
		result: toolsResult{
			Component:      c.componentID,
			ReleaseVersion: ctx.releaseVersion,
			SourceAsset:    c.archivePath,
			Destination:    dest,
			Action:         action,
			Managed:        true,
		},
		setEntry: entry,
	}
}

func (c skillComponent) uninstall(ctx toolsContext) toolsOutcome {
	if ctx.entry == nil {
		dest := ""
		if repRoot, err := resolveRepresentativeRoot(ctx.repoRoot, ctx.commonDir); err == nil {
			dest = c.destPath(repRoot)
		}
		return toolsOutcome{result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionNotInstalled,
			Reason:      "no install metadata; nothing to remove",
		}}
	}

	dest := ctx.entry.DestinationPath

	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return toolsOutcome{
				result: toolsResult{
					Component:   c.componentID,
					Destination: dest,
					Action:      actionRemoved,
					Managed:     true,
					Reason:      "destination file was already absent; metadata cleared",
				},
				deleteEntry: true,
			}
		}
		return toolsOutcome{result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionFailed,
			Reason:      fmt.Sprintf("read destination: %v", err),
		}}
	}

	if sha256hex(data) != ctx.entry.Checksum {
		return toolsOutcome{result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionSkipped,
			Managed:     false,
			Reason:      "destination file was modified outside git-kura; not removing",
		}}
	}

	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return toolsOutcome{result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionFailed,
			Reason:      fmt.Sprintf("remove destination: %v", err),
		}}
	}

	// Remove the git-kura skill directory if it is now empty.
	// The parent (.claude / .agents) is user-managed and is never removed.
	_ = os.Remove(filepath.Dir(dest))

	return toolsOutcome{
		result: toolsResult{
			Component:   c.componentID,
			Destination: dest,
			Action:      actionRemoved,
			Managed:     true,
		},
		deleteEntry: true,
	}
}

// resolveRepresentativeRoot returns the repository root that git-kura uses as
// the stable installation target across all worktrees sharing the same common
// dir. When repoRoot is the main (non-managed) worktree, it is itself the
// representative root. When repoRoot is a git-kura managed worktree, the kura
// metadata records the representative root, which is read and validated.
//
// Error reasons begin with a stable token that scripts can match:
//   - missing-repository-metadata: inside a managed worktree but kura metadata is absent
//   - representative-root-missing: resolved path does not exist on disk
//   - representative-root-not-directory: resolved path is a file, not a directory
//   - representative-root-common-dir-mismatch: resolved path has a different common dir
func resolveRepresentativeRoot(repoRoot, commonDir string) (string, error) {
	worktreesBase := filepath.Join(commonDir, "kura", "worktrees")

	rel, relErr := filepath.Rel(worktreesBase, repoRoot)
	isManaged := relErr == nil &&
		rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!strings.ContainsRune(rel, filepath.Separator)

	var representativeRoot string
	if !isManaged {
		representativeRoot = repoRoot
	} else {
		key := rel
		metaPath := filepath.Join(commonDir, "kura", "meta", "worktrees", key+".json")
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("missing-repository-metadata: kura metadata for worktree %q not found", key)
			}
			return "", fmt.Errorf("missing-repository-metadata: read kura metadata: %w", err)
		}
		var meta struct {
			RepositoryRoot string `json:"repositoryRoot"`
		}
		if unmarshalErr := json.Unmarshal(metaData, &meta); unmarshalErr != nil || meta.RepositoryRoot == "" {
			return "", fmt.Errorf("missing-repository-metadata: parse kura metadata for worktree %q", key)
		}
		representativeRoot = meta.RepositoryRoot
	}

	info, err := os.Stat(representativeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("representative-root-missing: %q does not exist", representativeRoot)
		}
		return "", fmt.Errorf("representative-root-missing: stat %q: %w", representativeRoot, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("representative-root-not-directory: %q is not a directory", representativeRoot)
	}

	repCommonDir, err := gitutil.CommonDir(representativeRoot)
	if err != nil {
		return "", fmt.Errorf("representative-root-common-dir-mismatch: resolve common dir of %q: %w", representativeRoot, err)
	}
	if !samePathSafe(repCommonDir, commonDir) {
		return "", fmt.Errorf("representative-root-common-dir-mismatch: %q has common dir %q, expected %q", representativeRoot, repCommonDir, commonDir)
	}

	return representativeRoot, nil
}
