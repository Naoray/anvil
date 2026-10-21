package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/cli"
	"github.com/naoray/anvil/internal/procs"
)

// These variables are set at build time via -ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	if code, ok := procs.Supervise(os.Args); ok {
		os.Exit(code)
	}
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
		var apiErr *agentapi.Error
		if errors.As(err, &apiErr) {
			fmt.Fprintln(os.Stderr, "error: "+apiErr.Message)
			os.Exit(apiErr.ExitCode())
		}
		os.Exit(1)
	}
}
