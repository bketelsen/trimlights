// Package auth owns household OIDC login, opaque sessions and CSRF enforcement.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"trimlights/internal/storage"
)

const sessionCookie = "__Host-trimlight-session"
const flowCookie = "__Host-trimlight-flow"

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
}

func New(ctx context.Context, c Config, store *storage.Store) (*Auth, error) {
	return newWithClient(ctx, c, store, &http.Client{Timeout: 10 * time.Second})
}

func newWithClient(ctx context.Context, c Config, store *storage.Store, client *http.Client) (*Auth, error) {
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
	u, _ := url.Parse(c.Origin)
	return &Auth{config: c, store: store, host: u.Host, client: client, now: time.Now, verifier: provider.Verifier(&oidc.Config{ClientID: c.ClientID}), oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: c.Origin + "/auth/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}}, nil
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
func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	state, browser, nonce, verifier := rand.Text(), rand.Text(), rand.Text(), oauth2.GenerateVerifier()
	f := storage.LoginFlow{StateHash: hash(state), BrowserHash: hash(browser), Nonce: nonce, Verifier: verifier, Expires: a.now().Add(10 * time.Minute)}
	if err := a.store.SaveFlow(r.Context(), f); err != nil {
		http.Error(w, "Login temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	cookie(w, flowCookie, browser, 600)
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
}
func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state, code := q.Get("state"), q.Get("code")
	browser, err := r.Cookie(flowCookie)
	if err != nil || state == "" || len(state) > 256 || code == "" || len(code) > 8192 {
		http.Error(w, "Invalid login callback", 400)
		return
	}
	if iss := q.Get("iss"); iss != "" && iss != a.config.Issuer {
		http.Error(w, "Unexpected identity provider", 400)
		return
	}
	flow, err := a.store.ConsumeFlow(r.Context(), hash(state), hash(browser.Value), a.now())
	cookie(w, flowCookie, "", -1)
	if err != nil {
		http.Error(w, "Login expired or already used", 400)
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.client)
	token, err := a.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		http.Error(w, "Identity provider rejected login", http.StatusUnauthorized)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Missing identity token", http.StatusUnauthorized)
		return
	}
	identity, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		http.Error(w, "Invalid identity token", http.StatusUnauthorized)
		return
	}
	if identity.Nonce != flow.Nonce || !a.allowed(identity.Subject) {
		http.Error(w, "Household access denied", http.StatusForbidden)
		return
	}
	var claims struct {
		Name string `json:"name"`
	}
	if err = identity.Claims(&claims); err != nil {
		http.Error(w, "Invalid identity claims", http.StatusUnauthorized)
		return
	}
	id := rand.Text()
	session := storage.Session{Hash: a.sessionHash(id), Subject: identity.Subject, Name: claims.Name, CSRF: rand.Text(), Expires: a.now().Add(12 * time.Hour)}
	if err = a.store.SaveSession(r.Context(), session); err != nil {
		http.Error(w, "Could not save login", http.StatusServiceUnavailable)
		return
	}
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
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
				http.Error(w, "Invalid request origin or CSRF token", http.StatusForbidden)
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
				http.Error(w, "Could not log out", http.StatusServiceUnavailable)
				return
			}
			cookie(w, sessionCookie, "", -1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(401)
	_, _ = w.Write([]byte(`{"error":"sign_in_required"}`))
}
