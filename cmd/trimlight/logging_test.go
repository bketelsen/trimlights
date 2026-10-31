package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"

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

// TestServeStartupFailureLogOmitsProviderData makes OIDC discovery fail with
// provider data that carries a marker. The JSON log keeps only a category.
func TestServeStartupFailureLogOmitsProviderData(t *testing.T) {
	const marker = "leakmarker5k"
	mux := http.NewServeMux()
	provider := httptest.NewTLSServer(mux)
	defer provider.Close()
	mode := ""
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "body":
			http.Redirect(w, r, "/moved?leak="+marker, http.StatusFound)
		case "drop":
			http.Redirect(w, r, "/drop?leak="+marker, http.StatusFound)
		case "issuer":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issuer":"https://` + marker + `.test","authorization_endpoint":"https://` + marker + `.test/a","token_endpoint":"https://` + marker + `.test/t","jwks_uri":"https://` + marker + `.test/k"}`))
		}
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, marker+" discovery body", 500)
	})
	mux.HandleFunc("/drop", func(w http.ResponseWriter, r *http.Request) {
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.Close()
		}
	})
	old := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = old })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret"), []byte("client-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]any{"origin": "https://lights.example.test", "issuer": provider.URL, "client_id": "fixture", "client_secret_file": "secret", "allowed_subjects": []string{"owner"}})
	if err != nil {
		t.Fatal(err)
	}
	oidcPath := filepath.Join(dir, "oidc.json")
	if err = os.WriteFile(oidcPath, config, 0600); err != nil {
		t.Fatal(err)
	}
	for i, m := range []string{"body", "drop", "issuer", "body", "drop", "issuer"} {
		mode = m
		args := []string{"serve", "--listen", "127.0.0.1:0", "--data-dir", filepath.Join(dir, fmt.Sprint("data-", i)), "--oidc-config", oidcPath}
		if i >= 3 {
			args = append(args, "--json")
		}
		out, errs, err := execute(t, args...)
		if err == nil {
			t.Fatal(m, "discovery failure accepted")
		}
		// Everything that serve writes reaches the container log: check it all.
		if strings.Contains(strings.ToLower(out+errs+err.Error()), marker) {
			t.Fatal(m, "provider data reached the output:", out, errs, err)
		}
		if !strings.Contains(out+errs, "household login setup failed") {
			t.Fatal(m, "no operator message", out, errs)
		}
		var stopped map[string]any
		for _, line := range strings.Split(errs, "\n") {
			if !strings.HasPrefix(line, "{") {
				continue // The operator message is not JSON.
			}
			var entry map[string]any
			if err = json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatal(line)
			}
			if entry["msg"] == "trimlight stopped" {
				stopped = entry
			}
		}
		if stopped == nil || stopped["error"] != "household login setup failed" || stopped["level"] != "ERROR" {
			t.Fatal(m, stopped, errs)
		}
	}
}
