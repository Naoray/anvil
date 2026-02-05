package main

import (
	"os"

	"github.com/artisanexperiences/arbor/internal/cli"
)

// These variables are set at build time via -ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	cli.Version = Version
	cli.Commit = Commit
	cli.BuildDate = BuildDate
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
