package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndRestoreDryRunNeverOpenFiles(t *testing.T) {
	for _, cmd := range []string{"backup", "restore"} {
		dest := filepath.Join(t.TempDir(), "new")
		args := []string{cmd, "--output", dest, "--dry-run", "--json"}
		if cmd == "backup" {
			args = append(args, "--data-dir", "missing-data", "--oidc-config", "missing-config")
		} else {
			args = append(args, "--input", "missing-backup")
		}
		out, _, err := execute(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		var r map[string]any
		if json.Unmarshal([]byte(out), &r) != nil || r["verified"] != false || r["dry_run"] != true {
			t.Fatal(out)
		}
		if _, err = os.Stat(dest); !os.IsNotExist(err) {
			t.Fatal("dry run wrote output")
		}
	}
}
func TestServeDryRunRestoreMarkerOverridesHost(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("0123456789012345678901234567890123"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "controller-disabled"), []byte("restore"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRIMLIGHT_CONTROLLER_HOST", "fixture.example.invalid")
	out, _, err := execute(t, "serve", "--data-dir", dir, "--api-token-file", token, "--host", "another.example.invalid", "--dry-run", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if json.Unmarshal([]byte(out), &r) != nil || r["controller_enabled"] != false || r["restore_guarded"] != true {
		t.Fatal(out)
	}
}
