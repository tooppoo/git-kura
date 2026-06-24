package main

import (
	"os"

	"github.com/tooppoo/git-kura/internal/cli"
)

// resolve by goreleaser
// https://goreleaser.com/resources/cookbooks/using-main.version/
var version string = "0.1.0"

func main() {
	os.Exit(int(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version)))
}
