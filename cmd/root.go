// Package cmd wires the costctl command tree.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set via -ldflags at build time. Defaults to "dev" for local builds.
var Version = "dev"

// Global flags accessible from subcommands.
var (
	flagJSON    bool
	flagVerbose bool
	flagQuiet   bool
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "costctl",
	Short: "Multi-cloud cost and pricing CLI",
	Long: `costctl queries cloud price catalogs for current and historical pricing.

The current command set covers Azure VM spot pricing:
  - Current prices via the Azure Retail Prices API (unauthenticated)
  - ~90 days of history via cloudprice.net (needs a subscription key)

Get a cloudprice.net key at https://developer.cloudprice.net/ and store it with:
  costctl config set-key cloudprice <KEY>`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute is the package entrypoint called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "emit JSON to stdout instead of human-readable text")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "show extra diagnostics on stderr")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "suppress progress on stderr")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable color output (also honors NO_COLOR)")
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("costctl {{.Version}}\n")
}
