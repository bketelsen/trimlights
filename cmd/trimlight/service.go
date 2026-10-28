package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
	"trimlights/internal/auth"
	"trimlights/internal/service"
	"trimlights/internal/trimlight"
)

func readToken(path string) (string, error) {
	if path == "" {
		return "", errors.New("--api-token-file is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	b, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 || len(b) > 4096 || strings.ContainsAny(token, "\r\n\t ") {
		return "", errors.New("token must be a single value of 32–4096 characters")
	}
	return token, nil
}

func serveCommand() *cobra.Command {
	var listen, dir, tokenFile, host, tz, oidcFile string
	var offset int
	cmd := &cobra.Command{Use: "serve", Short: "Run the local development API and serialized controller worker"}
	f := cmd.Flags()
	f.StringVar(&listen, "listen", "127.0.0.1:8787", "loopback listen address")
	f.StringVar(&dir, "data-dir", "", "local persistent data directory (required)")
	f.StringVar(&tokenFile, "api-token-file", "", "file containing a private API token (required)")
	f.StringVar(&oidcFile, "oidc-config", "", "household OIDC JSON configuration; replaces local bearer mode")
	f.StringVar(&host, "host", "", "controller hostname; defaults to TRIMLIGHT_CONTROLLER_HOST; empty disables access")
	f.StringVar(&tz, "timezone", "America/New_York", "controller timezone")
	f.IntVar(&offset, "daily-schedules-offset", -1, "explicit captured schedule offset")
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		host = resolveControllerHost(cmd, host)
		if len(args) != 0 {
			return errors.New("positional arguments are not supported")
		}
		var token string
		var household *auth.Config
		var err error
		if oidcFile != "" {
			if tokenFile != "" {
				return errors.New("choose --oidc-config or --api-token-file")
			}
			household, err = readOIDC(oidcFile)
		} else {
			token, err = readToken(tokenFile)
		}
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return err
		}
		if offset < -1 {
			return errors.New("schedule offset must be -1 or nonnegative")
		}
		c := service.Config{Listen: listen, DataDir: dir, APIToken: token, ScheduleOffset: offset, OIDC: household}
		if host != "" {
			c.Controller = trimlight.NewClient(net.JoinHostPort(host, "8586"), loc)
		}
		if err = service.Validate(c); err != nil {
			return err
		}
		if clix.DryRun {
			return result(cmd, map[string]any{"dry_run": true, "listen": listen, "controller_enabled": host != ""}, "Configuration valid; no listener, database or controller connection opened.")
		}
		return service.Run(cmd.Context(), c)
	})
	return cmd
}

func apiCommand(refresh bool) *cobra.Command {
	name, description := "status", "Read cached service state without contacting the controller"
	if refresh {
		name, description = "refresh", "Ask the service to refresh controller state (may sync its clock)"
	}
	var base, tokenFile string
	var acknowledge bool
	cmd := &cobra.Command{Use: name, Short: description}
	cmd.Flags().StringVar(&base, "url", "http://127.0.0.1:8787", "local service URL")
	cmd.Flags().StringVar(&tokenFile, "api-token-file", "", "private API token file")
	if refresh {
		cmd.Flags().BoolVar(&acknowledge, "acknowledge-clock-sync", false, "allow snapshot request to synchronize controller clock")
	}
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			return errors.New("positional arguments are not supported")
		}
		u, err := url.Parse(base)
		if err != nil {
			return err
		}
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
			return errors.New("local API URL must be http://loopback-address:port")
		}
		if refresh && !acknowledge {
			return errors.New("refresh requires --acknowledge-clock-sync")
		}
		token, err := readToken(tokenFile)
		if err != nil {
			return err
		}
		if clix.DryRun {
			return result(cmd, map[string]any{"dry_run": true, "command": name}, "Dry run: no service or controller request sent.")
		}
		path, method, body := "/api/v1/status", http.MethodGet, []byte(nil)
		if refresh {
			path, method, body = "/api/v1/controller/refresh", http.MethodPost, []byte(`{"acknowledge_clock_sync":true}`)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 22*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if refresh {
			req.Header.Set("Content-Type", "application/json")
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil}}
		defer client.CloseIdleConnections()
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			return err
		}
		if response.StatusCode != 200 {
			return fmt.Errorf("service returned %s: %s", response.Status, string(raw))
		}
		if !json.Valid(raw) {
			return errors.New("service returned invalid JSON")
		}
		// Text mode still prints readable state; JSON mode emits exactly one document.
		return result(cmd, json.RawMessage(raw), strings.TrimSpace(string(raw)))
	})
	return cmd
}

func readOIDC(path string) (*auth.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var input struct {
		auth.Config
		ClientSecretFile string `json:"client_secret_file"`
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if err = d.Decode(&input); err != nil {
		return nil, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected one OIDC configuration object")
	}
	if input.ClientSecretFile == "" {
		return nil, errors.New("client_secret_file is required")
	}
	secretPath := input.ClientSecretFile
	if !filepath.IsAbs(secretPath) {
		secretPath = filepath.Join(filepath.Dir(path), secretPath)
	}
	secret, err := os.Open(secretPath)
	if err != nil {
		return nil, err
	}
	defer secret.Close()
	b, err := io.ReadAll(io.LimitReader(secret, 4097))
	if err != nil {
		return nil, err
	}
	if len(b) > 4096 {
		return nil, errors.New("OIDC secret file is too large")
	}
	input.ClientSecret = strings.TrimSpace(string(b))
	if err = input.Config.Validate(); err != nil {
		return nil, err
	}
	return &input.Config, nil
}
