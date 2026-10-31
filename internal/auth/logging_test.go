package auth

import (
	"bytes"
	"encoding/base64"
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
	tampered.Value = tamper(t, flow.Value)
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
		{"sign-in verification failed", map[string]any{"reason": "invalid_signature"}},
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

// tamper flips 1 bit in the sealed bytes, so the value always differs.
func tamper(t *testing.T, value string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) == 0 {
		t.Fatal("flow cookie is not sealed base64", err)
	}
	raw[len(raw)/2] ^= 1
	out := base64.RawURLEncoding.EncodeToString(raw)
	if out == value {
		t.Fatal("tampered value did not change")
	}
	return out
}

// TestSignInFailureLogsUseFixedCategories feeds provider data that carries a
// marker into every failure path. Only fixed categories may reach the log.
func TestSignInFailureLogsUseFixedCategories(t *testing.T) {
	for _, tc := range []struct {
		mode  string
		attrs map[string]any
	}{
		{"oauth-marker", map[string]any{"msg": "sign-in token exchange failed", "reason": "idp_rejected", "idp_status": float64(400), "oauth_error": "unknown"}},
		{"oauth-allowed", map[string]any{"msg": "sign-in token exchange failed", "reason": "idp_rejected", "idp_status": float64(400), "oauth_error": "invalid_grant"}},
		{"redirect", map[string]any{"msg": "sign-in token exchange failed", "reason": "transport"}},
		{"expires", map[string]any{"msg": "sign-in token exchange failed", "reason": "invalid_token_response"}},
		{"malformed-id", map[string]any{"msg": "sign-in verification failed", "reason": "malformed_id_token"}},
		{"issuer-marker", map[string]any{"msg": "sign-in verification failed", "reason": "issuer_mismatch"}},
		{"jwks", map[string]any{"msg": "sign-in verification failed", "reason": "jwks_unavailable"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			// A fresh fixture has no cached keys, so the key set is fetched.
			a, f := authFixture(t)
			logs := captureLogs(a)
			path, flow := begin(t, a, f)
			f.mu.Lock()
			f.invalid = tc.mode
			f.mu.Unlock()
			if got := call(a, "GET", path, flow, "", "").Code; got != 401 {
				t.Fatal(got)
			}
			if strings.Contains(strings.ToLower(logs.String()), leakMarker) {
				t.Fatal("provider data reached the log:", logs.String())
			}
			msg := tc.attrs["msg"].(string)
			if !findLog(logEntries(t, logs), msg, tc.attrs) {
				t.Fatal("missing category", tc.attrs, logs.String())
			}
			for _, e := range logEntries(t, logs) {
				if _, ok := e["error"]; ok {
					t.Fatal("raw error logged", e)
				}
			}
		})
	}
}
