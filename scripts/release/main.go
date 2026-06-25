package main

import (
	"fmt"
	"os"

	"github.com/tooppoo/git-kura/scripts/release/internal/cmd"
	"github.com/tooppoo/git-kura/scripts/release/internal/step/placeholder"
)

func main() {
	registry := placeholder.NewDefaultRegistry()
	root := cmd.NewRootCommand(registry)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
