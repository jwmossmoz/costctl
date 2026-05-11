package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Inspect or clear the on-disk response cache",
	Long: `costctl caches successful cloudprice.net responses on disk so repeat
queries (and the next morning's dashboard run) are free.

Cache location: $XDG_CACHE_HOME/costctl  (default ~/.cache/costctl)
Override with COSTCTL_CACHE_DIR=<path>.`,
}

var cacheShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the cache directory path, entry count, and total size",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveCacheDir()
		if err != nil {
			return err
		}
		entries, total, err := summarizeCache(dir)
		if err != nil {
			return err
		}
		fmt.Printf("path:     %s\n", dir)
		fmt.Printf("entries:  %d\n", entries)
		fmt.Printf("bytes:    %d (%.1f KiB)\n", total, float64(total)/1024)
		return nil
	},
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete all cached responses",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveCacheDir()
		if err != nil {
			return err
		}
		removed, err := clearCache(dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "removed %d cache entries from %s\n", removed, dir)
		return nil
	},
}

func resolveCacheDir() (string, error) {
	if p := os.Getenv("COSTCTL_CACHE_DIR"); p != "" {
		return p, nil
	}
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "costctl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "costctl"), nil
}

func summarizeCache(dir string) (entries int, total int64, err error) {
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries++
		total += info.Size()
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, nil
	}
	return entries, total, err
}

func clearCache(dir string) (int, error) {
	removed := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		if err := os.Remove(path); err == nil {
			removed++
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return removed, err
}

// touched returns the most recent modification time below dir, for testing.
// Currently unused outside tests but kept here so the surface is reviewable.
func cacheLastModified(dir string) (time.Time, error) { //nolint:unused
	var latest time.Time
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest, err
}

func init() {
	cacheCmd.AddCommand(cacheShowCmd, cacheClearCmd)
	rootCmd.AddCommand(cacheCmd)
}
