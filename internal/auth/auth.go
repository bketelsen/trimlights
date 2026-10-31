// Package auth owns household OIDC login, opaque sessions and CSRF enforcement.
package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"trimlights/internal/storage"
)

const sessionCookie = "__Host-trimlight-session"
const flowCookie = "__Host-trimlight-flow"
const flowLife = 10 * time.Minute
const maxFlowCookie = 1024

type Config struct {
	Origin          string   `json:"origin"`
	Issuer          string   `json:"issuer"`
	ClientID        string   `json:"client_id"`
	ClientSecret    string   `json:"-"`
	AllowedSubjects []string `json:"allowed_subjects"`
}

func (c Config) Validate() error {
	for _, raw := range []string{c.Origin, c.Issuer} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("origin and issuer must be absolute HTTPS URLs")
		}
	}
	u, _ := url.Parse(c.Origin)
	if u.Path != "" {
		return errors.New("origin must not include a path or trailing slash")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("OIDC client ID and secret are required")
	}
	if len(c.AllowedSubjects) == 0 {
		return errors.New("at least one allowed OIDC subject is required")
	}
	for _, sub := range c.AllowedSubjects {
		if strings.TrimSpace(sub) == "" {
			return errors.New("allowed subjects must not be empty")
		}
	}
	return nil
}

type Auth struct {
	config   Config
	store    *storage.Store
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	host     string
	client   *http.Client
	now      func() time.Time
	// flowAEAD seals pending login state into the flow cookie. Its key exists only
	// in memory, so a restart invalidates pending logins and nothing anonymous is stored.
	flowAEAD cipher.AEAD
	// spent holds state hashes of flows that a callback holds or exchanged, until
	// the flow expires. An entry stays only after the IdP accepts the code.
	spentMu sync.Mutex
	spent   map[string]time.Time
	// log records sign-in outcomes. It must never receive codes, state, nonces,
	// verifiers, tokens, cookies, CSRF values, session hashes or the client secret.
	log *slog.Logger
}

// pendingFlow is the sealed content of the flow cookie.
type pendingFlow struct {
	StateHash string `json:"s"`
	Browser   string `json:"b"`
	Nonce     string `json:"n"`
	Verifier  string `json:"v"`
	Expires   int64  `json:"e"`
}

// New prepares household login. A nil logger uses slog.Default.
func New(ctx context.Context, c Config, store *storage.Store, logger *slog.Logger) (*Auth, error) {
	return newWithClient(ctx, c, store, &http.Client{Timeout: 10 * time.Second}, logger)
}

func newWithClient(ctx context.Context, c Config, store *storage.Store, client *http.Client, logger *slog.Logger) (*Auth, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), c.Issuer)
	if err != nil {
		return nil, err
	}
	for _, raw := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, errors.New("OIDC endpoints must use HTTPS")
		}
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(c.Origin)
	return &Auth{config: c, store: store, host: u.Host, client: client, now: time.Now, flowAEAD: aead, spent: map[string]time.Time{}, log: logger, verifier: provider.Verifier(&oidc.Config{ClientID: c.ClientID}), oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: c.Origin + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}}, nil
}
func hash(value string) string { v := sha256.Sum256([]byte(value)); return hex.EncodeToString(v[:]) }
func (a *Auth) sessionHash(value string) string {
	return hash(a.config.Issuer + "\x00" + a.config.ClientID + "\x00" + a.config.Origin + "\x00" + value)
}
func cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *Auth) allowed(sub string) bool {
	for _, v := range a.config.AllowedSubjects {
		if sub == v {
			return true
		}
	}
	return false
}
func (a *Auth) sealFlow(f pendingFlow) (string, error) {
	plain, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.flowAEAD.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(a.flowAEAD.Seal(nonce, nonce, plain, []byte(flowCookie))), nil
}

// openFlow authenticates and decodes a flow cookie. It refuses tampered, foreign
// and expired values.
func (a *Auth) openFlow(value string) (pendingFlow, error) {
	var f pendingFlow
	invalid := errors.New("invalid login flow")
	if len(value) > maxFlowCookie {
		return f, invalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	size := a.flowAEAD.NonceSize()
	if err != nil || len(raw) < size+a.flowAEAD.Overhead() {
		return f, invalid
	}
	plain, err := a.flowAEAD.Open(nil, raw[:size], raw[size:], []byte(flowCookie))
	if err != nil || json.Unmarshal(plain, &f) != nil || f.StateHash == "" || f.Browser == "" || f.Nonce == "" || f.Verifier == "" {
		return pendingFlow{}, invalid
	}
	if !time.Unix(f.Expires, 0).After(a.now()) {
		return pendingFlow{}, invalid
	}
	return f, nil
}

// claim reserves a flow for one token exchange. It refuses a flow that another
// callback holds or already exchanged, and prunes expired entries.
func (a *Auth) claim(f pendingFlow) bool {
	a.spentMu.Lock()
	defer a.spentMu.Unlock()
	now := a.now()
	for k, expires := range a.spent {
		if !expires.After(now) {
			delete(a.spent, k)
		}
	}
	if _, ok := a.spent[f.StateHash]; ok {
		return false
	}
	a.spent[f.StateHash] = time.Unix(f.Expires, 0)
	return true
}

// release drops a claim after the IdP refused the exchange, so that a failed
// anonymous callback leaves no entry behind.
func (a *Auth) release(f pendingFlow) {
	a.spentMu.Lock()
	defer a.spentMu.Unlock()
	delete(a.spent, f.StateHash)
}

// login keeps the pending flow in a sealed cookie, so an anonymous request
// writes nothing to SQLite and cannot exhaust a shared capacity.
func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	state, browser, nonce, verifier := rand.Text(), rand.Text(), rand.Text(), oauth2.GenerateVerifier()
	sealed, err := a.sealFlow(pendingFlow{StateHash: hash(state), Browser: browser, Nonce: nonce, Verifier: verifier, Expires: a.now().Add(flowLife).Unix()})
	if err != nil {
		a.log.ErrorContext(r.Context(), "sign-in flow unavailable", "status", 503, "error", err)
		http.Error(w, "Login temporarily unavailable", 503)
		return
	}
	cookie(w, flowCookie, sealed, int(flowLife/time.Second))
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
}
func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	cookie(w, flowCookie, "", -1)
	q := r.URL.Query()
	state, code := q.Get("state"), q.Get("code")
	browser, err := r.Cookie(flowCookie)
	if err != nil || state == "" || len(state) > 256 || code == "" || len(code) > 8192 {
		a.refused(r, "malformed_callback")
		http.Error(w, "Invalid login callback", 400)
		return
	}
	if iss := q.Get("iss"); iss != "" && iss != a.config.Issuer {
		a.refused(r, "unexpected_issuer")
		http.Error(w, "Unexpected identity provider", 400)
		return
	}
	flow, err := a.openFlow(browser.Value)
	if err != nil {
		a.refused(r, "invalid_or_expired_flow")
		http.Error(w, "Login expired or already used", 400)
		return
	}
	if subtle.ConstantTimeCompare([]byte(flow.StateHash), []byte(hash(state))) != 1 {
		a.refused(r, "state_mismatch")
		http.Error(w, "Login expired or already used", 400)
		return
	}
	if !a.claim(flow) {
		a.refused(r, "flow_already_used")
		http.Error(w, "Login expired or already used", 400)
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.client)
	token, err := a.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		a.release(flow)
		a.log.WarnContext(r.Context(), "sign-in token exchange failed", exchangeFailure(err)...)
		http.Error(w, "Identity provider rejected login", 401)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		a.log.WarnContext(r.Context(), "sign-in verification failed", "reason", "missing_id_token")
		http.Error(w, "Missing identity token", 401)
		return
	}
	identity, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		a.log.WarnContext(r.Context(), "sign-in verification failed", "reason", verifyFailure(err))
		http.Error(w, "Invalid identity token", 401)
		return
	}
	if identity.Nonce != flow.Nonce {
		a.log.WarnContext(r.Context(), "sign-in verification failed", "reason", "nonce_mismatch")
		http.Error(w, "Household access denied", http.StatusForbidden)
		return
	}
	if !a.allowed(identity.Subject) {
		a.log.WarnContext(r.Context(), "sign-in denied", "subject", identity.Subject)
		http.Error(w, "Household access denied", http.StatusForbidden)
		return
	}
	var claims struct {
		Name string `json:"name"`
	}
	if err = identity.Claims(&claims); err != nil {
		a.log.WarnContext(r.Context(), "sign-in verification failed", "reason", "invalid_claims", "subject", identity.Subject)
		http.Error(w, "Invalid identity claims", 401)
		return
	}
	id := rand.Text()
	session := storage.Session{Hash: a.sessionHash(id), Subject: identity.Subject, Name: claims.Name, CSRF: rand.Text(), Expires: a.now().Add(12 * time.Hour)}
	if err = a.store.SaveSession(r.Context(), session); err != nil {
		a.log.ErrorContext(r.Context(), "sign-in session not saved", "status", 503, "subject", identity.Subject, "error", err)
		http.Error(w, "Could not save login", 503)
		return
	}
	a.log.InfoContext(r.Context(), "sign-in succeeded", "subject", identity.Subject)
	cookie(w, sessionCookie, id, 12*60*60)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type sessionKey struct{}

func Identity(ctx context.Context) (storage.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(storage.Session)
	return s, ok
}
func (a *Auth) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Host != a.host {
			http.Error(w, "Unexpected host", 421)
			return
		}
		if r.URL.Path == "/auth/login" || r.URL.Path == "/auth/callback" {
			if r.Method != "GET" {
				http.Error(w, "Method not allowed", 405)
				return
			}
			if r.URL.Path == "/auth/login" {
				a.login(w, r)
			} else {
				a.callback(w, r)
			}
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || len(c.Value) > 256 {
			unauthorized(w)
			return
		}
		session, err := a.store.Session(r.Context(), a.sessionHash(c.Value), a.now())
		if err != nil || !a.allowed(session.Subject) {
			unauthorized(w)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != a.config.Origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
				http.Error(w, "Invalid request origin or CSRF token", 403)
				return
			}
		}
		if r.URL.Path == "/api/v1/me" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(session)
			return
		}
		if r.URL.Path == "/api/v1/logout" && r.Method == "POST" {
			if err = a.store.DeleteSession(r.Context(), session.Hash); err != nil {
				a.log.ErrorContext(r.Context(), "sign-out failed", "status", 503, "subject", session.Subject, "error", err)
				http.Error(w, "Could not log out", 503)
				return
			}
			cookie(w, sessionCookie, "", -1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}

// refused records why a callback was refused, as a category only. The callback
// parameters and the flow cookie stay out of the log.
func (a *Auth) refused(r *http.Request, reason string) {
	a.log.WarnContext(r.Context(), "sign-in callback refused", "reason", reason)
}

// oauthErrors are the RFC 6749 section 5.2 token error codes. Any other code
// comes from the response body unchecked, so it is logged as "unknown".
var oauthErrors = map[string]bool{"invalid_request": true, "invalid_client": true, "invalid_grant": true, "unauthorized_client": true, "unsupported_grant_type": true, "invalid_scope": true}

// exchangeFailure describes a failed token exchange with fixed categories only.
// Raw errors can carry response bodies, claim values or URLs with queries.
func exchangeFailure(err error) []any {
	var retrieve *oauth2.RetrieveError
	if errors.As(err, &retrieve) {
		status := 0
		if retrieve.Response != nil {
			status = retrieve.Response.StatusCode
		}
		code := "unknown"
		if oauthErrors[retrieve.ErrorCode] {
			code = retrieve.ErrorCode
		}
		return []any{"reason", "idp_rejected", "idp_status", status, "oauth_error", code}
	}
	var transport *url.Error
	if errors.As(err, &transport) {
		return []any{"reason", "transport", "timeout", transport.Timeout()}
	}
	return []any{"reason", "invalid_token_response"}
}

// verifyFailure maps an ID token verification error to a fixed category. The
// error text itself can quote claims, key set response bodies or URLs.
func verifyFailure(err error) string {
	var expired *oidc.TokenExpiredError
	if errors.As(err, &expired) {
		return "expired_id_token"
	}
	text := err.Error()
	for _, c := range []struct{ fragment, reason string }{
		{"fetching keys", "jwks_unavailable"},
		{"get keys failed", "jwks_unavailable"},
		{"issued by a different provider", "issuer_mismatch"},
		{"expected audience", "audience_mismatch"},
		{"malformed jwt", "malformed_id_token"},
		{"failed to unmarshal claims", "malformed_id_token"},
		{"before the nbf", "id_token_not_yet_valid"},
		{"signature", "invalid_signature"},
		{"not signed", "invalid_signature"},
	} {
		if strings.Contains(text, c.fragment) {
			return c.reason
		}
	}
	return "invalid_id_token"
}
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(401)
	_, _ = w.Write([]byte(`{"error":"sign_in_required"}`))
}
