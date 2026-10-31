package auth

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func captureLogs(a *Auth) *bytes.Buffer {
	var b bytes.Buffer
	a.log = slog.New(slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &b
}

func logEntries(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal("log line is not JSON", line)
		}
		out = append(out, entry)
	}
	return out
}

func findLog(entries []map[string]any, msg string, attrs map[string]any) bool {
	for _, e := range entries {
		if e["msg"] != msg {
			continue
		}
		match := true
		for k, v := range attrs {
			if e[k] != v {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestSignInLogsOutcomesWithoutSecrets runs full sign-in flows against the
// simulated provider and proves that no credential material reaches the log.
func TestSignInLogsOutcomesWithoutSecrets(t *testing.T) {
	a, f := authFixture(t)
	logs := captureLogs(a)
	var secrets []string
	remember := func(path string, flow *http.Cookie) {
		u, err := url.Parse(path)
		if err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		secrets = append(secrets, u.Query().Get("state"), hash(u.Query().Get("state")), f.nonce, f.challenge, flow.Value)
		f.mu.Unlock()
	}

	// Success, then a replay, a sign-out and the provider's issued values.
	path, flow := begin(t, a, f)
	remember(path, flow)
	w := call(a, "GET", path, flow, "", "")
	session := sessionFrom(w)
	if w.Code != 303 || session == nil {
		t.Fatal(w.Code, w.Body.String())
	}
	f.mu.Lock()
	secrets = append(secrets, f.verifier, f.idToken)
	f.mu.Unlock()
	call(a, "GET", path, flow, "", "")
	me := call(a, "GET", "/api/v1/me", session, "", "")
	var identity struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &identity); err != nil || identity.CSRF == "" {
		t.Fatal(err, me.Body.String())
	}
	secrets = append(secrets, session.Value, a.sessionHash(session.Value), identity.CSRF)
	if got := call(a, "POST", "/api/v1/logout", session, a.config.Origin, identity.CSRF).Code; got != 204 {
		t.Fatal(got)
	}

	// A forged code fails the token exchange.
	path, flow = begin(t, a, f)
	remember(path, flow)
	if got := call(a, "GET", strings.Replace(path, "code="+testCode, "code=forged-Zk81", 1), flow, "", "").Code; got != 401 {
		t.Fatal(got)
	}
	secrets = append(secrets, "forged-Zk81")

	// A tampered flow cookie is refused before the exchange.
	tampered := *flow
	tampered.Value = "x" + flow.Value[1:]
	if got := call(a, "GET", path, &tampered, "", "").Code; got != 400 {
		t.Fatal(got)
	}

	// An outsider is denied, and a bad signature fails verification.
	for _, invalid := range []string{"subject", "signature"} {
		f.mu.Lock()
		f.invalid = invalid
		f.mu.Unlock()
		path, flow = begin(t, a, f)
		remember(path, flow)
		if got := call(a, "GET", path, flow, "", "").Code; got != 401 && got != 403 {
			t.Fatal(invalid, got)
		}
		f.mu.Lock()
		secrets = append(secrets, f.verifier, f.idToken)
		f.mu.Unlock()
	}

	entries := logEntries(t, logs)
	for _, want := range []struct {
		msg   string
		attrs map[string]any
	}{
		{"sign-in succeeded", map[string]any{"subject": "owner", "level": "INFO"}},
		{"sign-in callback refused", map[string]any{"reason": "flow_already_used"}},
		{"sign-in callback refused", map[string]any{"reason": "invalid_or_expired_flow"}},
		{"sign-in token exchange failed", map[string]any{"idp_status": float64(400)}},
		{"sign-in denied", map[string]any{"subject": "outsider", "level": "WARN"}},
		{"sign-in verification failed", map[string]any{"reason": "invalid_id_token"}},
	} {
		if !findLog(entries, want.msg, want.attrs) {
			t.Fatal("missing log", want, logs.String())
		}
	}
	text := logs.String()
	secrets = append(secrets, testCode, testSecret, testAccessToken)
	for _, secret := range secrets {
		if secret == "" {
			t.Fatal("fixture lost a value to check")
		}
		if strings.Contains(text, secret) {
			t.Fatalf("log contains credential material %q:\n%s", secret, text)
		}
	}
}
