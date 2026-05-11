// Package cmd wires the costctl command tree.
package cmd

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	"github.com/jwmossmoz/costctl/internal/cloudprice"
)

// Version is set via -ldflags at build time (Makefile / goreleaser). When the
// binary is produced by plain `go install`, ldflags don't run; in that case we
// fall back to the module version embedded by the Go toolchain.
var Version = "dev"

// newCloudpriceClient builds a cloudprice client honoring global cache flags.
func newCloudpriceClient(key string) *cloudprice.Client {
	c := cloudprice.New(key)
	c.UseCache = !flagNoCache
	c.CacheTTL = flagCacheTTL
	return c
}

func resolveVersion() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return Version
	}
	return info.Main.Version
}

// Global flags accessible from subcommands.
var (
	flagJSON     bool
	flagVerbose  bool
	flagQuiet    bool
	flagNoColor  bool
	flagNoCache  bool
	flagCacheTTL time.Duration
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
	rootCmd.PersistentFlags().BoolVar(&flagNoCache, "no-cache", false, "disable on-disk response cache for this run")
	rootCmd.PersistentFlags().DurationVar(&flagCacheTTL, "cache-ttl", cloudprice.DefaultCacheTTL,
		"response cache freshness window")
	rootCmd.Version = resolveVersion()
	rootCmd.SetVersionTemplate("costctl {{.Version}}\n")
}
