package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostyard/clix"
)

// lockedBuffer lets the test read stderr while the service writes it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serveUntilStarted runs serve until its startup line appears. The controller
// host is a reserved name that the service must never contact at startup.
func serveUntilStarted(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	var errs lockedBuffer
	old := clix.Stdout
	clix.Stdout = &out
	t.Cleanup(func() { clix.Stdout = old })
	root := rootCommand()
	root.SetOut(&out)
	root.SetErr(&errs)
	root.SetArgs(append([]string{"serve", "--listen", "127.0.0.1:0"}, args...))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- (&clix.App{Version: "test", Commit: "abc"}).RunContext(ctx, root)
	}()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(errs.String(), "trimlight serving") {
		select {
		case err := <-done:
			t.Fatal("serve ended before startup", err, errs.String())
		case <-deadline:
			t.Fatal("no startup line", errs.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err, errs.String())
	}
	logs := errs.String()
	if strings.Contains(logs, "controller-host.invalid") {
		t.Fatal("log contains the controller address", logs)
	}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal("log line is not JSON", line)
		}
		if entry["msg"] == "trimlight serving" {
			return entry
		}
	}
	t.Fatal("startup line missing", logs)
	return nil
}

func TestServeStartupLogOmitsControllerHost(t *testing.T) {
	parent := t.TempDir()
	token := filepath.Join(parent, "api.token")
	if err := os.WriteFile(token, []byte(strings.Repeat("a", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(parent, "data")
	entry := serveUntilStarted(t, "--data-dir", data, "--api-token-file", token, "--host", "controller-host.invalid")
	if entry["level"] != "INFO" || entry["mode"] != "bearer-development" || entry["controller_enabled"] != true || entry["restore_guard"] != false || !strings.HasPrefix(entry["listen"].(string), "127.0.0.1:") {
		t.Fatal(entry)
	}
	for _, key := range []string{"version", "commit"} {
		if _, ok := entry[key]; !ok {
			t.Fatal("startup line lacks", key, entry)
		}
	}
	// The restore guard disables the controller, and the line says so.
	if err := os.WriteFile(filepath.Join(data, "controller-disabled"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	entry = serveUntilStarted(t, "--data-dir", data, "--api-token-file", token, "--host", "controller-host.invalid", "--log-level", "debug")
	if entry["controller_enabled"] != false || entry["restore_guard"] != true {
		t.Fatal(entry)
	}
}

func TestServeLogLevelFlag(t *testing.T) {
	for _, value := range []string{"debug", "info", "warn", "error"} {
		if _, err := parseLogLevel(value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"", "INFO", "trace", "info+2"} {
		if _, err := parseLogLevel(value); err == nil {
			t.Fatal("accepted", value)
		}
	}
	token := filepath.Join(t.TempDir(), "api.token")
	if err := os.WriteFile(token, []byte(strings.Repeat("a", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(t, "serve", "--data-dir", t.TempDir(), "--api-token-file", token, "--log-level", "loud", "--dry-run"); err == nil {
		t.Fatal("invalid log level accepted")
	}
}
