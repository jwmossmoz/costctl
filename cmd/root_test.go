package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestExitCodeForError(t *testing.T) {
	if got := exitCodeForError(usageErrorf("bad flag")); got != 2 {
		t.Fatalf("usage error exit code = %d, want 2", got)
	}
	if got := exitCodeForError(errors.New("network failed")); got != 1 {
		t.Fatalf("runtime error exit code = %d, want 1", got)
	}
}

func TestValidateAzureSpotHistoryFlags(t *testing.T) {
	oldSKU, oldOS, oldTier := flagSKU, flagOS, flagTier
	t.Cleanup(func() {
		flagSKU, flagOS, flagTier = oldSKU, oldOS, oldTier
	})

	flagSKU = "Standard_F8s_v2"
	flagOS = "linux"
	flagTier = "burstable"
	err := validateAzureSpotHistoryFlags()
	if err == nil {
		t.Fatal("expected invalid tier error, got nil")
	}
	if got := exitCodeForError(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestValidateOSFilterRejectsUnknownValues(t *testing.T) {
	err := validateOSFilter("freebsd")
	if err == nil {
		t.Fatal("expected invalid OS error, got nil")
	}
	if got := exitCodeForError(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestValidateGCPSpotHistoryFlagsRejectsBadDays(t *testing.T) {
	oldMachine, oldRegion, oldDays := flagMachine, flagGCPRegion, flagDays
	t.Cleanup(func() {
		flagMachine, flagGCPRegion, flagDays = oldMachine, oldRegion, oldDays
	})

	flagMachine = "n2-standard-2"
	flagGCPRegion = "us-central1"
	flagDays = 91
	err := validateGCPSpotHistoryFlags()
	if err == nil {
		t.Fatal("expected invalid days error, got nil")
	}
	if got := exitCodeForError(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
}

func TestRunCacheShowJSON(t *testing.T) {
	oldJSON := flagJSON
	t.Cleanup(func() { flagJSON = oldJSON })
	flagJSON = true

	dir := t.TempDir()
	t.Setenv("COSTCTL_CACHE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "entry.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := runCacheShow(cmd, nil); err != nil {
		t.Fatalf("runCacheShow: %v", err)
	}

	var got cacheSummary
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v; body=%q", err, out.String())
	}
	if got.Path != dir || got.Entries != 1 || got.Bytes != 2 {
		t.Fatalf("summary = %+v, want path=%q entries=1 bytes=2", got, dir)
	}
}
