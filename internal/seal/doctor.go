package seal

import (
	"errors"
	"fmt"
	"sort"
)

// DoctorInput is the input for the Doctor usecase.
type DoctorInput struct {
	RepoRoot string
}

// Doctor validates the seal store for the given repo root.
// It is repository-wide and read-only; it does not acquire the store lock.
func Doctor(input DoctorInput) (DoctorResult, error) {
	storeFile, _, err := StorePaths(input.RepoRoot)
	if err != nil {
		return DoctorResult{}, fmt.Errorf("resolve seal store path: %w", err)
	}
	inspection, err := InspectStore(storeFile)
	if err != nil {
		return DoctorResult{}, err
	}

	errorCount := 0
	warningCount := 0
	for _, f := range inspection.Findings {
		if f.Severity == "error" {
			errorCount++
		} else {
			warningCount++
		}
	}
	findings := inspection.Findings
	if findings == nil {
		findings = []DoctorFinding{}
	}
	return DoctorResult{
		Healthy: len(inspection.Findings) == 0,
		Summary: DoctorSummary{
			CheckedClaims: inspection.CheckedClaims,
			ErrorCount:    errorCount,
			WarningCount:  warningCount,
		},
		Findings: findings,
	}, nil
}

// StoreInspection holds the result of a store integrity check.
type StoreInspection struct {
	CheckedClaims int
	Findings      []DoctorFinding
}

// InspectStore reads the seal store at storePath and returns structured findings.
// An error is returned only when the store cannot be read or parsed.
func InspectStore(storePath string) (StoreInspection, error) {
	store, err := ReadStore(storePath)
	if err != nil {
		return StoreInspection{}, err
	}
	return InspectPathStore(store), nil
}

// InspectPathStore checks an already-read store for integrity violations.
func InspectPathStore(store PathStore) StoreInspection {
	rawPaths := make([]string, 0, len(store.Paths))
	for rawPath := range store.Paths {
		rawPaths = append(rawPaths, rawPath)
	}
	sort.Strings(rawPaths)

	var findings []DoctorFinding
	seen := make(map[string]string, len(rawPaths))
	for _, rawPath := range rawPaths {
		p := rawPath
		entry := store.Paths[rawPath]

		canonical, canonErr := CanonicalStoredPath(rawPath)
		if canonErr != nil {
			findings = append(findings, DoctorFinding{
				Severity: "error",
				Code:     "invalid-stored-path",
				Path:     &p,
				Message:  canonErr.Error(),
			})
			continue
		}
		if firstRawPath, ok := seen[canonical]; ok {
			firstKey := store.Paths[firstRawPath].Key
			var msg string
			if firstKey != entry.Key {
				msg = fmt.Sprintf("store entries %q (key %q) and %q (key %q) refer to the same canonical path %q",
					firstRawPath, firstKey, rawPath, entry.Key, canonical)
			} else {
				msg = fmt.Sprintf("store entries %q and %q duplicate canonical path %q", firstRawPath, rawPath, canonical)
			}
			findings = append(findings, DoctorFinding{
				Severity: "error",
				Code:     "duplicate-canonical-path",
				Path:     &p,
				Message:  msg,
			})
			continue
		}
		seen[canonical] = rawPath
		if canonical != rawPath {
			findings = append(findings, DoctorFinding{
				Severity: "error",
				Code:     "non-normalized-path",
				Path:     &p,
				Message:  fmt.Sprintf("store entry %q is not normalized; canonical path is %q", rawPath, canonical),
			})
		}
	}

	return StoreInspection{
		CheckedClaims: len(rawPaths),
		Findings:      findings,
	}
}

// DoctorStore validates the seal store at storePath.
// Returns nil when the store is healthy or absent; returns an error listing all violations otherwise.
func DoctorStore(storePath string) error {
	inspection, err := InspectStore(storePath)
	if err != nil {
		return err
	}
	errs := make([]error, 0, len(inspection.Findings))
	for _, f := range inspection.Findings {
		errs = append(errs, fmt.Errorf("%s", f.Message))
	}
	return errors.Join(errs...)
}
