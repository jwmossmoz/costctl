package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cfg "github.com/jwmossmoz/costctl/internal/config"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage costctl's on-disk configuration",
	Long: `Config is stored at $XDG_CONFIG_HOME/costctl/config.json
(defaults to ~/.config/costctl/config.json) with mode 0600.

Override the path with COSTCTL_CONFIG=<path>.`,
}

var configSetKeyCmd = &cobra.Command{
	Use:   "set-key <provider> <key>",
	Short: "Store an API key for a provider (e.g. cloudprice)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, key := strings.ToLower(args[0]), args[1]
		path, err := cfg.SetKey(provider, key)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote key for %q to %s\n", provider, path)
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the current config (keys are masked)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, path, err := cfg.Load()
		if err != nil {
			return err
		}
		masked := map[string]any{
			"path":      path,
			"providers": maskedProviders(c),
		}
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(masked)
		}
		fmt.Printf("config file: %s\n", path)
		if len(c.Providers) == 0 {
			fmt.Println("no providers configured")
			return nil
		}
		fmt.Println("providers:")
		for name, p := range c.Providers {
			fmt.Printf("  %-12s api_key=%s\n", name, maskKey(p.APIKey))
		}
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the resolved config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := cfg.Path()
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	},
}

func maskedProviders(c *cfg.Config) map[string]map[string]string {
	out := map[string]map[string]string{}
	for name, p := range c.Providers {
		out[name] = map[string]string{"api_key": maskKey(p.APIKey)}
	}
	return out
}

func maskKey(k string) string {
	if k == "" {
		return "(unset)"
	}
	if len(k) <= 8 {
		return "********"
	}
	return k[:4] + "..." + k[len(k)-4:]
}

func init() {
	configCmd.AddCommand(configSetKeyCmd, configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
