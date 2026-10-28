package main

import (
	"errors"
	"fmt"
	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
	"trimlights/internal/backup"
)

func backupCommand(restore bool) *cobra.Command {
	name, description := "backup", "Create a private recovery bundle from live local data and OIDC configuration"
	if restore {
		name, description = "restore", "Restore a recovery bundle into a new directory with controller access disabled"
	}
	var dir, oidc, input, output string
	cmd := &cobra.Command{Use: name, Short: description}
	cmd.Flags().StringVar(&output, "output", "", "new output archive (backup) or directory (restore)")
	if restore {
		cmd.Flags().StringVar(&input, "input", "", "private backup archive")
	} else {
		cmd.Flags().StringVar(&dir, "data-dir", "", "existing local data directory")
		cmd.Flags().StringVar(&oidc, "oidc-config", "", "OIDC JSON configuration to include with its secret")
	}
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 || output == "" || restore && input == "" || !restore && (dir == "" || oidc == "") {
			return errors.New("required paths are missing; see command --help")
		}
		if clix.DryRun {
			return result(cmd, map[string]any{"dry_run": true, "command": name, "output": output, "verified": false}, "Dry run: no files, database or network opened; backup contents remain unverified.")
		}
		var report backup.Report
		var err error
		if restore {
			report, err = backup.Restore(cmd.Context(), input, output)
		} else {
			config, e := readOIDC(oidc)
			if e != nil {
				return e
			}
			report, err = backup.Create(cmd.Context(), dir, output, *config, version+"/"+commit)
		}
		if err != nil {
			return err
		}
		message := fmt.Sprintf("%s complete: %s (%d photos, %d pattern revisions, %d calendar revisions, %d house revisions).", name, output, report.Photos, report.Patterns, report.Schedules, report.Layouts)
		if restore {
			message += " Controller access is guarded off; browser sign-in is required."
		} else {
			message += " Contains private photos and the OIDC secret; store privately."
		}
		return result(cmd, report, message)
	})
	return cmd
}
