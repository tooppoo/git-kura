package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

// Component IDs for built-in skill components.
const (
	ClaudeSkillComponentID = "claude-skill"
	CodexSkillComponentID  = "codex-skill"
)

// Archive-relative paths for each skill file inside the tools archive.
const (
	ClaudeSkillArchivePath = "claude-skill/SKILL.md"
	CodexSkillArchivePath  = "codex-skill/SKILL.md"
)

// NewClaudeSkillComponent returns the claude-skill component.
func NewClaudeSkillComponent() SkillComponent {
	return SkillComponent{
		componentID: ClaudeSkillComponentID,
		archivePath: ClaudeSkillArchivePath,
		destPath: func(representativeRoot string) string {
			return filepath.Join(representativeRoot, ".claude", "skills", "git-kura", "SKILL.md")
		},
	}
}

// NewCodexSkillComponent returns the codex-skill component.
func NewCodexSkillComponent() SkillComponent {
	return SkillComponent{
		componentID: CodexSkillComponentID,
		archivePath: CodexSkillArchivePath,
		destPath: func(representativeRoot string) string {
			return filepath.Join(representativeRoot, ".agents", "skills", "git-kura", "SKILL.md")
		},
	}
}

// SkillComponent is the shared implementation for claude-skill and codex-skill.
type SkillComponent struct {
	componentID string
	archivePath string
	destPath    func(representativeRoot string) string
}

func (c SkillComponent) ID() string { return c.componentID }

func (c SkillComponent) Status(ctx Context) Outcome {
	repRoot, err := resolveRepresentativeRoot(ctx.RepoRoot, ctx.CommonDir)
	if err != nil {
		return Outcome{Result: Result{Component: c.componentID, Action: ActionFailed, Reason: err.Error()}}
	}

	dest := c.destPath(repRoot)

	if ctx.Entry == nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return Outcome{Result: Result{
				Component: c.componentID, Destination: dest,
				Action: ActionNotInstalled, Managed: false, Reason: "unmanaged-file-exists",
			}}
		}
		return Outcome{Result: Result{
			Component: c.componentID, Destination: dest,
			Action: ActionNotInstalled, Managed: false, Reason: "not-installed",
		}}
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return Outcome{Result: Result{
				Component: c.componentID, ReleaseVersion: ctx.Entry.InstalledVersion,
				Destination: dest, Action: ActionNotInstalled, Managed: true,
				Reason: "destination file missing",
			}}
		}
		return Outcome{Result: Result{
			Component: c.componentID, ReleaseVersion: ctx.Entry.InstalledVersion,
			Destination: dest, Action: ActionFailed,
			Reason: fmt.Sprintf("read destination: %v", err),
		}}
	}

	if SHA256Hex(data) != ctx.Entry.Checksum {
		return Outcome{Result: Result{
			Component: c.componentID, ReleaseVersion: ctx.Entry.InstalledVersion,
			Destination: dest, Action: ActionInstalled, Managed: false,
			Reason: "file was modified outside git-kura",
		}}
	}

	return Outcome{Result: Result{
		Component: c.componentID, ReleaseVersion: ctx.Entry.InstalledVersion,
		Destination: dest, Action: ActionInstalled, Managed: true,
	}}
}

func (c SkillComponent) Install(ctx InstallContext) Outcome {
	repRoot, err := resolveRepresentativeRoot(ctx.RepoRoot, ctx.CommonDir)
	if err != nil {
		return Outcome{Result: Result{
			Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
			Action: ActionFailed, Reason: err.Error(),
		}}
	}

	dest := c.destPath(repRoot)

	fail := func(reason string) Outcome {
		return Outcome{Result: Result{
			Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
			Destination: dest, Action: ActionFailed, Reason: reason,
		}}
	}

	assetComp, ok := ctx.Asset.ComponentAssets(c.componentID)
	if !ok {
		return fail(fmt.Sprintf("component %q not found in tools asset", c.componentID))
	}

	expectedSum, ok := assetComp.Files[c.archivePath]
	if !ok {
		return fail(fmt.Sprintf("archive path %q not in component manifest", c.archivePath))
	}

	data, err := os.ReadFile(ctx.Asset.Path(c.archivePath))
	if err != nil {
		return fail(fmt.Sprintf("read asset file: %v", err))
	}

	if SHA256Hex(data) != expectedSum {
		return fail("asset file checksum mismatch")
	}

	if ctx.Entry == nil {
		if _, statErr := os.Stat(dest); statErr == nil {
			return fail("unmanaged-file-exists: destination file exists but is not managed by git-kura")
		}
	}

	if ctx.Entry != nil {
		existing, readErr := os.ReadFile(dest)
		if readErr == nil {
			existingSum := SHA256Hex(existing)
			if existingSum != ctx.Entry.Checksum {
				return fail("destination file was modified outside git-kura; not overwriting")
			}
			if existingSum == expectedSum {
				return Outcome{Result: Result{
					Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
					SourceAsset: c.archivePath, Destination: dest,
					Action: ActionSkipped, Managed: true, Reason: "already installed; checksum matches",
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

	action := ActionCreated
	if ctx.Entry != nil {
		action = ActionUpdated
	}

	now := time.Now().UTC().Format(time.RFC3339)
	created := now
	if ctx.Entry != nil {
		created = ctx.Entry.CreatedAt
	}

	entry := &MetadataEntry{
		Component:        c.componentID,
		SourceAssetID:    c.archivePath,
		ReleaseVersion:   ctx.ReleaseVersion,
		InstalledVersion: ctx.ReleaseVersion,
		DestinationPath:  dest,
		Checksum:         expectedSum,
		ManagedMode:      ManagedModeFile,
		CreatedAt:        created,
		UpdatedAt:        now,
	}

	return Outcome{
		Result: Result{
			Component: c.componentID, ReleaseVersion: ctx.ReleaseVersion,
			SourceAsset: c.archivePath, Destination: dest, Action: action, Managed: true,
		},
		SetEntry: entry,
	}
}

func (c SkillComponent) Uninstall(ctx Context) Outcome {
	if ctx.Entry == nil {
		dest := ""
		if repRoot, err := resolveRepresentativeRoot(ctx.RepoRoot, ctx.CommonDir); err == nil {
			dest = c.destPath(repRoot)
		}
		return Outcome{Result: Result{
			Component: c.componentID, Destination: dest,
			Action: ActionNotInstalled, Reason: "no install metadata; nothing to remove",
		}}
	}

	dest := ctx.Entry.DestinationPath

	data, err := os.ReadFile(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return Outcome{
				Result: Result{
					Component: c.componentID, Destination: dest,
					Action: ActionRemoved, Managed: true,
					Reason: "destination file was already absent; metadata cleared",
				},
				DeleteEntry: true,
			}
		}
		return Outcome{Result: Result{
			Component: c.componentID, Destination: dest,
			Action: ActionFailed, Reason: fmt.Sprintf("read destination: %v", err),
		}}
	}

	if SHA256Hex(data) != ctx.Entry.Checksum {
		return Outcome{Result: Result{
			Component: c.componentID, Destination: dest,
			Action: ActionSkipped, Managed: false,
			Reason: "destination file was modified outside git-kura; not removing",
		}}
	}

	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return Outcome{Result: Result{
			Component: c.componentID, Destination: dest,
			Action: ActionFailed, Reason: fmt.Sprintf("remove destination: %v", err),
		}}
	}

	_ = os.Remove(filepath.Dir(dest))

	return Outcome{
		Result:      Result{Component: c.componentID, Destination: dest, Action: ActionRemoved, Managed: true},
		DeleteEntry: true,
	}
}

// SamePathSafe reports whether a and b refer to the same filesystem path.
func SamePathSafe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// resolveRepresentativeRoot returns the repository root used as the stable
// installation target across all worktrees sharing the same common dir.
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
	if !SamePathSafe(repCommonDir, commonDir) {
		return "", fmt.Errorf("representative-root-common-dir-mismatch: %q has common dir %q, expected %q", representativeRoot, repCommonDir, commonDir)
	}

	return representativeRoot, nil
}
