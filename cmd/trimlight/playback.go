package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
	"trimlights/internal/trimlight"
)

func playbackVerificationCommand() *cobra.Command {
	var host, tz, output, baseline string
	var target int
	cmd := &cobra.Command{Use: "verify-playback", Short: "Bounded OFF/saved-slot/TIMER test with baseline restoration"}
	cmd.Flags().StringVar(&host, "host", "", "controller host; defaults to TRIMLIGHT_CONTROLLER_HOST")
	cmd.Flags().StringVar(&tz, "timezone", "America/New_York", "controller local timezone")
	cmd.Flags().StringVar(&output, "output", "", "new private evidence directory (required)")
	cmd.Flags().StringVar(&baseline, "baseline-sha256", "", "reviewed snapshot fingerprint (required for writes)")
	cmd.Flags().IntVar(&target, "pattern-id", 0, "existing saved slot to test, 1–18 (required)")
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		host = resolveControllerHost(cmd, host)
		if len(args) != 0 || host == "" || output == "" || target < 1 || target > 18 {
			return errors.New("verify-playback requires --host, --output, --pattern-id 1–18 and no positional arguments")
		}
		location, err := time.LoadLocation(tz)
		if err != nil {
			return err
		}
		if clix.DryRun {
			return result(cmd, map[string]any{
				"dry_run": true, "command": cmd.Name(), "host": host, "pattern_id": target,
				"baseline_verified": false, "clock_sync_possible": true,
				"sequence": []string{"off", "saved-slot", "original-effect", "timer"},
			}, "Dry run: no connection or files; hardware baseline remains unverified.")
		}
		decoded, err := hex.DecodeString(baseline)
		if err != nil || len(decoded) != 32 {
			return errors.New("verify-playback requires --baseline-sha256 from a reviewed snapshot")
		}
		if err = os.Mkdir(output, 0700); err != nil {
			return fmt.Errorf("create new evidence directory: %w", err)
		}
		client := trimlight.NewClient(net.JoinHostPort(host, "8586"), location)
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		report, verifyErr := client.VerifyPlayback(ctx, baseline, byte(target), func(stage string, state *trimlight.Snapshot) error {
			if err := save(filepath.Join(output, stage+".json"), state); err != nil {
				return err
			}
			if !clix.Silent && !clix.JSONOutput {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Playback verification: %s readback recorded\n", stage)
			}
			return nil
		})
		errorText := ""
		if verifyErr != nil {
			errorText = verifyErr.Error()
		}
		evidence := struct {
			Report *trimlight.PlaybackReport `json:"report"`
			Error  string                    `json:"error,omitempty"`
		}{report, errorText}
		saveErr := save(filepath.Join(output, "result.json"), evidence)
		if err = errors.Join(verifyErr, saveErr); err != nil {
			return err
		}
		return result(cmd, evidence, "OFF, saved playback, TIMER restoration and independent readback verified; physical observation still required.")
	})
	return cmd
}
