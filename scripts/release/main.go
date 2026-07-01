package main

import (
	"fmt"
	"os"

	"github.com/tooppoo/git-kura/scripts/release/internal/cmd"
	"github.com/tooppoo/git-kura/scripts/release/internal/step"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/homebrew"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/placeholder"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/releaseasset"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/scoop"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/tag"
)

func main() {
	registry := placeholder.NewDefaultRegistry()
	// Replace placeholders with real handlers as each step is implemented.
	registry.Register(step.StepTag, tag.New())
	registry.Register(step.StepReleaseAsset, releaseasset.New())
	registry.Register(step.StepScoop, scoop.New())
	registry.Register(step.StepHomebrew, homebrew.New())
	root := cmd.NewRootCommand(registry)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
