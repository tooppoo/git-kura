package step

import "fmt"

type Step string

const (
	StepTag          Step = "tag"
	StepReleaseAsset Step = "release-asset"
	StepScoop        Step = "scoop"
	StepWinget       Step = "winget"
)

// knownSteps is the canonical list of recognised release steps.
var knownSteps = []Step{StepTag, StepReleaseAsset, StepScoop, StepWinget}

// Parse returns the Step for name, or an error if name is unknown.
func Parse(name string) (Step, error) {
	for _, s := range knownSteps {
		if string(s) == name {
			return s, nil
		}
	}
	return "", fmt.Errorf("unknown step %q: must be one of tag, release-asset, scoop, winget", name)
}
