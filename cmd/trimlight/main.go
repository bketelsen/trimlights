package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
	_ "time/tzdata"

	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
	_ "golang.org/x/crypto/x509roots/fallback" // Public CA roots for the scratch container.
	"trimlights/internal/trimlight"
)

var version, commit, date, builtBy string

func save(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	return errors.Join(encoder.Encode(value), f.Close())
}

// CLI output remains on the command goroutine. Concurrent services never use
// clix globals, which are read only while turning flags into configuration.
func result(cmd *cobra.Command, data any, message string) error {
	if clix.Silent {
		return nil
	}
	if written, err := clix.OutputJSON(data); written || err != nil {
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), message)
	return err
}

func commandRun(fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil && clix.JSONOutput && !clix.Silent {
			return clix.OutputJSONError(cmd.Name()+" failed", err)
		}
		return err
	}
}

func rootCommand() *cobra.Command {
	root := &cobra.Command{Use: "trimlight", Short: "Shared household lighting", SilenceUsage: true}
	root.AddCommand(diagnosticCommand(false), diagnosticCommand(true), playbackVerificationCommand(), serveCommand(), apiCommand(false), apiCommand(true), backupCommand(false), backupCommand(true), healthcheckCommand())
	return root
}

func diagnosticCommand(probe bool) *cobra.Command {
	name, description := "snapshot", "Read controller state (may synchronize its clock)"
	if probe {
		name, description = "probe-schedule2", "Bounded disabled-schedule write, restore and independent verification"
	}
	var host, tz, output, baseline string
	var offset int
	cmd := &cobra.Command{Use: name, Short: description}
	cmd.Flags().StringVar(&host, "host", "", "controller IP or hostname; defaults to TRIMLIGHT_CONTROLLER_HOST")
	cmd.Flags().StringVar(&tz, "timezone", "America/New_York", "controller local timezone")
	cmd.Flags().IntVar(&offset, "daily-schedules-offset", -1, "captured schedule offset; 3306 for validated controller")
	cmd.Flags().StringVar(&output, "output", "", "new JSON file for snapshot; new evidence directory for probe")
	if probe {
		cmd.Flags().StringVar(&baseline, "baseline-sha256", "", "reviewed snapshot stable_sha256 fingerprint (required for writes)")
	}
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		host = resolveControllerHost(cmd, host)
		if len(args) != 0 || host == "" {
			return errors.New("--host is required; positional arguments are not supported")
		}
		location, err := time.LoadLocation(tz)
		if err != nil {
			return err
		}
		if offset < -1 {
			return errors.New("schedule offset must be -1 or nonnegative")
		}
		if probe && output == "" {
			return errors.New("probe requires --output for a new evidence directory")
		}
		// No network, filesystem mutation or clock sync in dry-run. This describes
		// intent only; a dry probe cannot validate an unseen hardware baseline.
		if clix.DryRun {
			return result(cmd, map[string]any{"dry_run": true, "command": name, "host": host, "baseline_verified": false, "clock_sync_possible": true}, "Dry run: no connection or files; hardware baseline remains unverified.")
		}
		if probe {
			decoded, e := hex.DecodeString(baseline)
			if e != nil || len(decoded) != 32 {
				return errors.New("probe requires --baseline-sha256 from a reviewed snapshot")
			}
		}
		if output != "" {
			if _, err := os.Stat(output); err == nil {
				return errors.New("output exists; refusing to overwrite")
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		client := trimlight.NewClient(net.JoinHostPort(host, "8586"), location)
		if !probe {
			state, err := client.Snapshot(cmd.Context(), offset)
			if err != nil {
				return err
			}
			if output != "" {
				if err = save(output, state); err != nil {
					return err
				}
			}
			return result(cmd, state, fmt.Sprintf("Read %d patterns and %d daily schedules; clock synchronization possible.", len(state.Patterns), len(state.Schedules)))
		}
		if err = os.Mkdir(output, 0700); err != nil {
			return err
		}
		report, probeErr := client.ProbeSchedule2(cmd.Context(), baseline, func(stage string, state *trimlight.Snapshot) error {
			return save(filepath.Join(output, stage+".json"), state)
		})
		errorText := ""
		if probeErr != nil {
			errorText = probeErr.Error()
		}
		evidence := struct {
			Report *trimlight.ProbeReport `json:"report"`
			Error  string                 `json:"error,omitempty"`
		}{report, errorText}
		saveErr := save(filepath.Join(output, "result.json"), evidence)
		if err = errors.Join(probeErr, saveErr); err != nil {
			return err
		}
		return result(cmd, evidence, "Change, restoration and independent read-back verified.")
	})
	return cmd
}

func main() {
	app := clix.App{Version: version, Commit: commit, Date: date, BuiltBy: builtBy}
	if err := app.RunContext(context.Background(), rootCommand()); err != nil {
		os.Exit(1)
	}
}

// An explicit empty --host overrides the environment and disables the controller.
func resolveControllerHost(cmd *cobra.Command, value string) string {
	if !cmd.Flags().Changed("host") {
		return os.Getenv("TRIMLIGHT_CONTROLLER_HOST")
	}
	return value
}
