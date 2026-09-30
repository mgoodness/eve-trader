// Command eve-trader recommends station-trading buy/sell prices for the
// Rens trade station (spec §5). See internal/cli for the command tree and
// internal/engine for the pure recommendation logic.
package main

import (
	"fmt"
	"os"

	"github.com/mgoodness/eve-trader/internal/cli"
)

func main() {
	cfg, err := cli.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := cli.NewRootCmd(cfg)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
