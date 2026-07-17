package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/naoray/anvil/internal/cli"
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
		var childErr *cli.ChildExitError
		if errors.As(err, &childErr) {
			if childErr.Message != "" {
				fmt.Fprintln(os.Stderr, childErr.Message)
			}
			os.Exit(childErr.Code)
		}
		os.Exit(1)
	}
}
