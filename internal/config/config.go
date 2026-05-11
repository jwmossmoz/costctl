// Package config loads and saves costctl's on-disk configuration.
//
// File location follows the XDG Base Directory spec:
//
//	$XDG_CONFIG_HOME/costctl/config.json  (defaulting to ~/.config/costctl/config.json)
//
// API keys are namespaced by provider so the same file can hold creds for
// cloudprice, AWS, GCP, etc. as costctl grows.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ProviderCloudprice is the canonical provider name for cloudprice.net.
const ProviderCloudprice = "cloudprice"

// Config is the persisted on-disk shape.
type Config struct {
	Providers map[string]Provider `json:"providers"`
}

// Provider holds per-provider credentials.
type Provider struct {
	APIKey string `json:"api_key,omitempty"`
}

// Path returns the resolved config file path.
func Path() (string, error) {
	if p := os.Getenv("COSTCTL_CONFIG"); p != "" {
		return p, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "costctl", "config.json"), nil
}

// Load reads the config file. A missing file returns an empty Config, not an error.
func Load() (*Config, string, error) {
	path, err := Path()
	if err != nil {
		return nil, "", err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Providers: map[string]Provider{}}, path, nil
	}
	if err != nil {
		return nil, path, fmt.Errorf("reading %s: %w", path, err)
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, path, fmt.Errorf("parsing %s: %w", path, err)
	}
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	return &c, path, nil
}

// Save writes the config file, creating parent dirs with 0700 and the file with 0600.
func Save(c *Config) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, fmt.Errorf("creating config dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return path, fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return path, fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// SetKey writes a provider's API key to disk, preserving other providers.
func SetKey(provider, key string) (string, error) {
	c, _, err := Load()
	if err != nil {
		return "", err
	}
	p := c.Providers[provider]
	p.APIKey = key
	c.Providers[provider] = p
	return Save(c)
}

// ResolveAPIKey returns the API key for a provider with precedence
// flag > env > config file. Pass flagValue="" if no --api-key was set.
func ResolveAPIKey(provider, flagValue, envVar string) (key, source string) {
	if flagValue != "" {
		return flagValue, "flag"
	}
	if v := os.Getenv(envVar); v != "" {
		return v, "env:" + envVar
	}
	c, _, err := Load()
	if err == nil {
		if v := c.Providers[provider].APIKey; v != "" {
			return v, "config"
		}
	}
	return "", ""
}
