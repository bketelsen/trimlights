package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
)

const defaultHealthURL = "http://127.0.0.1:8787/readyz"

// healthcheckCommand serves container health checks in the scratch image,
// which has no shell or curl. It only contacts loopback addresses, ignores
// proxy settings and never follows redirects.
func healthcheckCommand() *cobra.Command {
	var target string
	var timeout time.Duration
	cmd := &cobra.Command{Use: "healthcheck", Short: "Exit 0 when a loopback readiness URL answers 200 OK", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&target, "url", defaultHealthURL, "loopback http(s) URL to check")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Second, "whole request deadline")
	cmd.RunE = commandRun(func(cmd *cobra.Command, args []string) error {
		u, err := loopbackURL(target)
		if err != nil {
			return err
		}
		if timeout <= 0 || timeout > time.Minute {
			return errors.New("timeout must be positive and at most 1m")
		}
		if clix.DryRun {
			return result(cmd, map[string]any{"dry_run": true, "command": "healthcheck", "url": u.String()}, "Dry run: no request sent.")
		}
		status, err := checkHealth(cmd.Context(), u, timeout)
		if err != nil {
			return err
		}
		return result(cmd, map[string]any{"url": u.String(), "status": status}, fmt.Sprintf("healthy: %s answered %d", u, status))
	})
	return cmd
}

func loopbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("--url must be an absolute http(s) URL without credentials")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("--url must name localhost or a loopback IP address")
	}
	return u, nil
}

func checkHealth(ctx context.Context, u *url.URL, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{
		// No proxy from the environment, so the request stays on loopback.
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("unhealthy: %s answered %d", u, resp.StatusCode)
	}
	return resp.StatusCode, nil
}
