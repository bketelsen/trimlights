package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
)

// clix owns process globals, so command tests are deliberately sequential.
func execute(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errs bytes.Buffer
	old := clix.Stdout
	clix.Stdout = &out
	t.Cleanup(func() { clix.Stdout = old })
	root := rootCommand()
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs(args)
	app := clix.App{Version: "test", Commit: "abc", Date: "2026-10-28", BuiltBy: "test"}
	err := app.RunContext(context.Background(), root)
	return out.String(), errs.String(), err
}

func TestCLIDryRunDoesNotTouchHardwareOrFiles(t *testing.T) {
	for _, command := range []string{"snapshot", "probe-schedule2"} {
		path := filepath.Join(t.TempDir(), "must-not-exist")
		out, _, err := execute(t, command, "--host", "invalid.invalid", "--output", path, "--dry-run", "--json")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err = json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err, out)
		}
		if got["dry_run"] != true || got["baseline_verified"] != false {
			t.Fatal(got)
		}
		if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry-run created output: %v", err)
		}
	}
}
func TestCLIErrorAndSilent(t *testing.T) {
	out, _, err := execute(t, "snapshot", "--json")
	if err == nil {
		t.Fatal("missing host accepted")
	}
	var got map[string]any
	if json.Unmarshal([]byte(out), &got) != nil || got["error"] != true {
		t.Fatal(out)
	}
	out, _, err = execute(t, "snapshot", "--host", "invalid.invalid", "--dry-run", "--json", "--silent")
	if err != nil || out != "" {
		t.Fatalf("silent: %q %v", out, err)
	}
}
func TestCLIHelpVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"snapshot", "--help"}, {"--version"}} {
		out, _, err := execute(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "trimlight") {
			t.Fatal(out)
		}
		if args[0] == "--version" && !strings.Contains(out, "abc") {
			t.Fatal(out)
		}
	}
}
func TestSaveNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.json")
	if err := save(path, "baseline"); err != nil {
		t.Fatal(err)
	}
	if err := save(path, "changed"); err == nil {
		t.Fatal("overwrote evidence")
	}
	got, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), "baseline") {
		t.Fatal(string(got), err)
	}
}

func TestServeDryRunAndConfigurationIsolation(t *testing.T) {
	parent := t.TempDir()
	tokenPath := filepath.Join(parent, "api.token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(parent, "not-created")
	out, _, err := execute(t, "serve", "--data-dir", data, "--api-token-file", tokenPath, "--dry-run", "--json")
	if err != nil || !strings.Contains(out, `"dry_run": true`) {
		t.Fatal(out, err)
	}
	if _, err = os.Stat(data); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run created data directory")
	}
	out, _, err = execute(t, "serve", "--data-dir", data, "--api-token-file", tokenPath, "--listen", "0.0.0.0:8787", "--dry-run", "--json")
	if err == nil || !strings.Contains(out, `"error": true`) {
		t.Fatal(out, err)
	}
}

func TestControllerHostEnvironmentAndExplicitOverride(t *testing.T) {
	t.Setenv("TRIMLIGHT_CONTROLLER_HOST", "fixture.example.invalid")
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"snapshot", "--dry-run", "--json"}, "fixture.example.invalid"}, {[]string{"snapshot", "--host", "override.example.invalid", "--dry-run", "--json"}, "override.example.invalid"}} {
		out, _, err := execute(t, tc.args...)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if json.Unmarshal([]byte(out), &got) != nil || got["host"] != tc.want {
			t.Fatal(out)
		}
	}
	token := filepath.Join(t.TempDir(), "api.token")
	if err := os.WriteFile(token, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		args := []string{"serve", "--api-token-file", token, "--data-dir", filepath.Join(t.TempDir(), "not-created"), "--dry-run", "--json"}
		if explicit {
			args = append(args, "--host=")
		}
		out, _, err := execute(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if json.Unmarshal([]byte(out), &got) != nil || got["controller_enabled"] == explicit {
			t.Fatal(out)
		}
	}
}
func TestProbeRequiresReviewedFingerprintBeforeConnecting(t *testing.T) {
	out, _, err := execute(t, "probe-schedule2", "--host", "invalid.invalid", "--output", filepath.Join(t.TempDir(), "evidence"), "--json")
	if err == nil || !strings.Contains(out, "baseline-sha256") {
		t.Fatal(out, err)
	}
}

func TestPlaybackVerificationCLI(t *testing.T) {
	for _, mode := range []string{"dry-run", "missing-fingerprint", "bad-target", "existing-directory"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "evidence")
			args := []string{"verify-playback", "--host", "invalid.invalid", "--output", path, "--pattern-id", "9", "--json"}
			switch mode {
			case "dry-run":
				args = append(args, "--dry-run")
			case "bad-target":
				args = append(args, "--pattern-id", "265")
			case "existing-directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--baseline-sha256", strings.Repeat("a", 64))
			}
			out, _, err := execute(t, args...)
			var got map[string]any
			if json.Unmarshal([]byte(out), &got) != nil {
				t.Fatal("expected one JSON document", out)
			}
			if mode == "dry-run" {
				if err != nil || got["dry_run"] != true || got["baseline_verified"] != false {
					t.Fatal(out, err)
				}
			} else if err == nil || got["error"] != true || strings.Contains(out, "lookup") {
				t.Fatal("expected local validation failure before connecting", out, err)
			}
			if mode != "existing-directory" {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("created evidence directory without valid inputs or during dry-run")
				}
			}
		})
	}
}
