package config

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("COSTCTL_CONFIG", path)
	return path
}

func TestPath_HonorsEnvOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "alt.json")
	t.Setenv("COSTCTL_CONFIG", want)
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestLoad_MissingFileReturnsEmptyConfig(t *testing.T) {
	withTempConfig(t)
	c, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Providers) != 0 {
		t.Errorf("expected empty providers, got %v", c.Providers)
	}
}

func TestSetKey_RoundTrip(t *testing.T) {
	path := withTempConfig(t)
	if _, err := SetKey("cloudprice", "secret-key-1"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config perms = %o, want 0600", info.Mode().Perm())
	}

	c, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := c.Providers["cloudprice"].APIKey; got != "secret-key-1" {
		t.Errorf("APIKey = %q, want %q", got, "secret-key-1")
	}
}

func TestSetKey_RepairsExistingFilePermissions(t *testing.T) {
	path := withTempConfig(t)
	if err := os.WriteFile(path, []byte(`{"providers":{}}`), 0o644); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if _, err := SetKey("cloudprice", "secret-key-1"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config perms = %o, want 0600", info.Mode().Perm())
	}
}

func TestSetKey_RepairsDefaultConfigDirPermissions(t *testing.T) {
	base := t.TempDir()
	t.Setenv("COSTCTL_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "costctl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed config dir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod config dir: %v", err)
	}

	if _, err := SetKey("cloudprice", "secret-key-1"); err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("config dir perms = %o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestSetKey_PreservesOtherProviders(t *testing.T) {
	withTempConfig(t)
	if _, err := SetKey("cloudprice", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetKey("future-cloud", "k2"); err != nil {
		t.Fatal(err)
	}
	c, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers["cloudprice"].APIKey != "k1" {
		t.Errorf("cloudprice key clobbered: %v", c.Providers)
	}
	if c.Providers["future-cloud"].APIKey != "k2" {
		t.Errorf("future-cloud key not written: %v", c.Providers)
	}
}

func TestResolveAPIKey_Precedence(t *testing.T) {
	withTempConfig(t)
	if _, err := SetKey("cloudprice", "from-config"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLOUDPRICE_API_KEY", "from-env")

	cases := []struct {
		name    string
		flag    string
		wantKey string
		wantSrc string
	}{
		{"flag wins over env+config", "from-flag", "from-flag", "flag"},
		{"env wins over config", "", "from-env", "env:CLOUDPRICE_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotKey, gotSrc, err := ResolveAPIKey("cloudprice", tc.flag, "CLOUDPRICE_API_KEY")
			if err != nil {
				t.Fatalf("ResolveAPIKey: %v", err)
			}
			if gotKey != tc.wantKey || gotSrc != tc.wantSrc {
				t.Errorf("ResolveAPIKey = (%q, %q); want (%q, %q)",
					gotKey, gotSrc, tc.wantKey, tc.wantSrc)
			}
		})
	}

	t.Run("config wins when flag+env empty", func(t *testing.T) {
		t.Setenv("CLOUDPRICE_API_KEY", "")
		gotKey, gotSrc, err := ResolveAPIKey("cloudprice", "", "CLOUDPRICE_API_KEY")
		if err != nil {
			t.Fatalf("ResolveAPIKey: %v", err)
		}
		if gotKey != "from-config" || gotSrc != "config" {
			t.Errorf("ResolveAPIKey = (%q, %q); want (%q, %q)",
				gotKey, gotSrc, "from-config", "config")
		}
	})

	t.Run("no key anywhere returns empty", func(t *testing.T) {
		withTempConfig(t) // fresh dir, nothing in it
		t.Setenv("CLOUDPRICE_API_KEY", "")
		gotKey, gotSrc, err := ResolveAPIKey("cloudprice", "", "CLOUDPRICE_API_KEY")
		if err != nil {
			t.Fatalf("ResolveAPIKey: %v", err)
		}
		if gotKey != "" || gotSrc != "" {
			t.Errorf("ResolveAPIKey = (%q, %q); want both empty", gotKey, gotSrc)
		}
	})
}

func TestResolveAPIKey_ReturnsConfigErrors(t *testing.T) {
	path := withTempConfig(t)
	if err := os.WriteFile(path, []byte(`{not-json`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	t.Setenv("CLOUDPRICE_API_KEY", "")
	if _, _, err := ResolveAPIKey("cloudprice", "", "CLOUDPRICE_API_KEY"); err == nil {
		t.Fatal("expected config parse error, got nil")
	}
}
