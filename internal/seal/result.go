package seal

import (
	"fmt"
	"io"
)

// ClaimPathItem is one path's result in a Claim operation.
type ClaimPathItem struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "claimed" or "already-owned"
}

// ClaimResult is the success payload for seal claim.
type ClaimResult struct {
	CurrentKey string          `json:"currentKey"`
	Paths      []ClaimPathItem `json:"paths"`
}

func (r ClaimResult) RenderHuman(w io.Writer) error {
	for _, p := range r.Paths {
		label := p.Status
		if p.Status == "already-owned" {
			label = "already owned"
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", label, p.Path); err != nil {
			return err
		}
	}
	return nil
}

// UnclaimPathItem is one path's result in an Unclaim operation.
type UnclaimPathItem struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "released" or "not-claimed"
}

// UnclaimResult is the success payload for seal unclaim.
type UnclaimResult struct {
	CurrentKey string            `json:"currentKey"`
	Paths      []UnclaimPathItem `json:"paths"`
}

func (r UnclaimResult) RenderHuman(w io.Writer) error {
	for _, p := range r.Paths {
		label := p.Status
		if p.Status == "not-claimed" {
			label = "not claimed"
		}
		if _, err := fmt.Fprintf(w, "%s: %s\n", label, p.Path); err != nil {
			return err
		}
	}
	return nil
}

// TestResultItem is one path's inspection result in a Test operation.
type TestResultItem struct {
	Path      string  `json:"path"`
	Status    string  `json:"status"`
	Safe      bool    `json:"safe"`
	ClaimedBy *string `json:"claimedBy"`
}

// TestResult is the success payload for seal test.
type TestResult struct {
	CurrentKey string           `json:"currentKey"`
	Passed     bool             `json:"passed"`
	Results    []TestResultItem `json:"results"`
}

func (r TestResult) RenderHuman(w io.Writer) error {
	for _, item := range r.Results {
		if item.Status == "claimed-by-other-key" {
			if _, err := fmt.Fprintf(w, "seal-conflict: path %q is already claimed by key %q\n", item.Path, *item.ClaimedBy); err != nil {
				return err
			}
		}
	}
	return nil
}

// LsClaim is one entry in the seal ls output.
type LsClaim struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// LsResult is the success payload for seal ls.
type LsResult struct {
	FilterKey *string   `json:"filterKey"`
	Claims    []LsClaim `json:"claims"`
}

func (r LsResult) RenderHuman(w io.Writer) error {
	for _, c := range r.Claims {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", c.Key, c.Path); err != nil {
			return err
		}
	}
	return nil
}

// DoctorFinding is one integrity finding from seal doctor.
type DoctorFinding struct {
	Severity string  `json:"severity"`
	Code     string  `json:"code"`
	Path     *string `json:"path"`
	Message  string  `json:"message"`
}

// DoctorSummary aggregates counts from a seal doctor inspection.
type DoctorSummary struct {
	CheckedClaims int `json:"checkedClaims"`
	ErrorCount    int `json:"errorCount"`
	WarningCount  int `json:"warningCount"`
}

// DoctorResult is the success payload for seal doctor.
type DoctorResult struct {
	Healthy  bool            `json:"healthy"`
	Summary  DoctorSummary   `json:"summary"`
	Findings []DoctorFinding `json:"findings"`
}

func (r DoctorResult) RenderHuman(w io.Writer) error {
	for _, f := range r.Findings {
		if _, err := fmt.Fprintf(w, "seal-doctor-error: %s\n", f.Message); err != nil {
			return err
		}
	}
	return nil
}
