package step

import (
	"fmt"
	"strings"
)

type Step string

const (
	StepTag          Step = "tag"
	StepReleaseAsset Step = "release-asset"
	StepScoop        Step = "scoop"
	StepWinget       Step = "winget"
)

// knownSteps is the canonical list of recognised release steps.
var knownSteps = []Step{StepTag, StepReleaseAsset, StepScoop, StepWinget}

// KnownNames returns the string names of all recognised steps.
func KnownNames() []string {
	names := make([]string, len(knownSteps))
	for i, s := range knownSteps {
		names[i] = string(s)
	}
	return names
}

// Parse returns the Step for name, or an error if name is unknown.
func Parse(name string) (Step, error) {
	for _, s := range knownSteps {
		if string(s) == name {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown step %q: must be one of %s", name, strings.Join(KnownNames(), ", "))
}
