package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
	"trimlights/internal/storage"
)

type providerFixture struct {
	mu                        sync.Mutex
	nonce, challenge, invalid string
	calls                     int
	key                       *rsa.PrivateKey
	issuer                    string
}

func (f *providerFixture) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	_ = r.ParseForm()
	user, password, ok := r.BasicAuth()
	if !ok || user != "client" || password != "secret" || r.Form.Get("code") != "valid" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != "https://lights.test/auth/callback" || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != f.challenge {
		http.Error(w, "bad token request", 400)
		return
	}
	claims := map[string]any{"iss": f.issuer, "sub": "owner", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": f.nonce, "name": "Household Owner"}
	switch f.invalid {
	case "nonce":
		claims["nonce"] = "wrong"
	case "subject":
		claims["sub"] = "outsider"
	case "audience":
		claims["aud"] = "wrong"
	case "issuer":
		claims["iss"] = "https://wrong.test"
	case "expiry":
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
	}
	key := f.key
	if f.invalid == "signature" {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		http.Error(w, "sign failed", 500)
		return
	}
	signed, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, "sign failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "never-expose-this", "token_type": "Bearer", "expires_in": 3600, "id_token": signed})
}
func authFixture(t *testing.T) (*Auth, *providerFixture) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &providerFixture{key: key}
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	f.issuer = server.URL
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "jwks_uri": server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", f.token)
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	a, err := newWithClient(context.Background(), Config{Origin: "https://lights.test", Issuer: server.URL, ClientID: "client", ClientSecret: "secret", AllowedSubjects: []string{"owner", "wife"}}, store, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return a, f
}
func call(a *Auth, method, path string, cookie *http.Cookie, origin, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://lights.test"+path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	a.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	return w
}
func begin(t *testing.T, a *Auth, f *providerFixture) (string, *http.Cookie) {
	t.Helper()
	w := call(a, "GET", "/auth/login", nil, "", "")
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := location.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Fatal(location)
	}
	f.mu.Lock()
	f.nonce = q.Get("nonce")
	f.challenge = q.Get("code_challenge")
	f.mu.Unlock()
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	return "/auth/callback?code=valid&state=" + url.QueryEscape(q.Get("state")), cookies[0]
}
func TestLoginSessionCSRFLogout(t *testing.T) {
	a, f := authFixture(t)
	path, flow := begin(t, a, f)
	if !flow.Secure || !flow.HttpOnly || flow.Domain != "" || flow.SameSite != http.SameSiteLaxMode || flow.Path != "/" {
		t.Fatal(flow)
	}
	w := call(a, "GET", path, flow, "", "")
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly {
		t.Fatal(session)
	}
	if got := call(a, "GET", path, flow, "", "").Code; got != 400 {
		t.Fatal("callback replay", got)
	}
	w = call(a, "GET", "/api/v1/me", session, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var me storage.Session
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Subject != "owner" || me.CSRF == "" || strings.Contains(w.Body.String(), session.Value) || strings.Contains(w.Body.String(), "never-expose") {
		t.Fatal(w.Body.String())
	}
	for _, pair := range [][2]string{{"", ""}, {a.config.Origin, "wrong"}, {"https://evil.test", me.CSRF}} {
		if got := call(a, "POST", "/api/v1/change", session, pair[0], pair[1]).Code; got != 403 {
			t.Fatal(got)
		}
	}
	if got := call(a, "POST", "/api/v1/change", session, a.config.Origin, me.CSRF).Code; got != 204 {
		t.Fatal(got)
	}
	if got := call(a, "POST", "/api/v1/logout", session, a.config.Origin, me.CSRF).Code; got != 204 {
		t.Fatal(got)
	}
	if got := call(a, "GET", "/api/v1/me", session, "", "").Code; got != 401 {
		t.Fatal(got)
	}
}
func TestIdentityValidation(t *testing.T) {
	a, f := authFixture(t)
	for _, invalid := range []string{"nonce", "subject", "audience", "issuer", "expiry", "signature"} {
		t.Run(invalid, func(t *testing.T) {
			f.mu.Lock()
			f.invalid = invalid
			f.mu.Unlock()
			path, c := begin(t, a, f)
			w := call(a, "GET", path, c, "", "")
			if w.Code != 401 && w.Code != 403 {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == sessionCookie {
					t.Fatal("invalid identity got session")
				}
			}
		})
	}
}
func TestStateBindingExpiryAndAuthorizationRemoval(t *testing.T) {
	a, f := authFixture(t)
	path, c := begin(t, a, f)
	wrong := *c
	wrong.Value = "wrong-browser"
	if got := call(a, "GET", path, &wrong, "", "").Code; got != 400 {
		t.Fatal(got)
	}
	if got := call(a, "GET", path+"wrong-state", c, "", "").Code; got != 400 {
		t.Fatal(got)
	}
	f.mu.Lock()
	count := f.calls
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("invalid state reached token endpoint")
	}
	stamp := time.Now()
	a.now = func() time.Time { return stamp.Add(11 * time.Minute) }
	if got := call(a, "GET", path, c, "", "").Code; got != 400 {
		t.Fatal(got)
	}
	a.now = time.Now
	path, c = begin(t, a, f)
	w := call(a, "GET", path, c, "", "")
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	a.config.AllowedSubjects = []string{"wife"}
	if got := call(a, "GET", "/api/v1/me", session, "", "").Code; got != 401 {
		t.Fatal(got)
	}
	a.config.AllowedSubjects = []string{"owner"}
	a.now = func() time.Time { return stamp.Add(13 * time.Hour) }
	if got := call(a, "GET", "/api/v1/me", session, "", "").Code; got != 401 {
		t.Fatal(got)
	}
	r := httptest.NewRequest("GET", "https://evil.test/auth/login", nil)
	rr := httptest.NewRecorder()
	a.Handler(http.NotFoundHandler()).ServeHTTP(rr, r)
	if rr.Code != 421 {
		t.Fatal(rr.Code)
	}
}

func sessionFrom(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			return c
		}
	}
	return nil
}

// flowCleared reports whether a callback response deletes the flow cookie.
func flowCleared(w *httptest.ResponseRecorder) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == flowCookie && c.MaxAge < 0 && c.Value == "" {
			return true
		}
	}
	return false
}

func TestAnonymousLoginFloodDoesNotBlockSignIn(t *testing.T) {
	a, f := authFixture(t)
	for i := 0; i < 101; i++ {
		if w := call(a, "GET", "/auth/login", nil, "", ""); w.Code != 303 {
			t.Fatal("anonymous login", i, w.Code, w.Body.String())
		}
	}
	path, flow := begin(t, a, f)
	w := call(a, "GET", path, flow, "", "")
	if w.Code != 303 || sessionFrom(w) == nil || !flowCleared(w) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSealedFlowCookieRefusals(t *testing.T) {
	a, f := authFixture(t)
	path, flow := begin(t, a, f)
	f.mu.Lock()
	nonce := f.nonce
	f.mu.Unlock()
	if strings.Contains(flow.Value, nonce) || len(flow.Value) > maxFlowCookie || flow.MaxAge != 600 {
		t.Fatal("flow cookie exposes state or has a wrong life", flow)
	}
	otherPath, other := begin(t, a, f)
	refused := func(name, path string, c *http.Cookie) {
		t.Helper()
		w := call(a, "GET", path, c, "", "")
		if w.Code != 400 || sessionFrom(w) != nil || !flowCleared(w) {
			t.Fatal(name, w.Code, w.Body.String())
		}
	}
	tampered := *flow
	raw := []byte(tampered.Value)
	if raw[len(raw)/2] == 'A' {
		raw[len(raw)/2] = 'B'
	} else {
		raw[len(raw)/2] = 'A'
	}
	tampered.Value = string(raw)
	refused("tampered", path, &tampered)
	refused("other browser", path, other)
	refused("wrong state", otherPath+"x", other)
	refused("missing", path, nil)
	stamp := time.Now()
	a.now = func() time.Time { return stamp.Add(10*time.Minute + time.Second) }
	refused("expired", path, flow)
	a.now = time.Now
	// A cookie sealed by another process, such as before a restart, is refused.
	b, _ := authFixture(t)
	foreign, err := b.sealFlow(pendingFlow{StateHash: hash("state"), Browser: "b", Nonce: "n", Verifier: "v", Expires: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	refused("foreign key", "/auth/callback?code=valid&state=state", &http.Cookie{Name: flowCookie, Value: foreign})
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 0 {
		t.Fatal("refused flow reached token endpoint", calls)
	}
	// The original flows still work, and each works once.
	f.mu.Lock()
	f.nonce, f.challenge = "", ""
	f.mu.Unlock()
	path, flow = begin(t, a, f)
	if w := call(a, "GET", path, flow, "", ""); w.Code != 303 || sessionFrom(w) == nil || !flowCleared(w) {
		t.Fatal(w.Code, w.Body.String())
	}
	refused("replay", path, flow)
}

func TestFailedExchangeDoesNotSpendFlow(t *testing.T) {
	a, f := authFixture(t)
	path, flow := begin(t, a, f)
	bad := strings.Replace(path, "code=valid", "code=forged", 1)
	if w := call(a, "GET", bad, flow, "", ""); w.Code != 401 || !flowCleared(w) {
		t.Fatal(w.Code, w.Body.String())
	}
	a.spentMu.Lock()
	entries := len(a.spent)
	a.spentMu.Unlock()
	if entries != 0 {
		t.Fatal("failed exchange left a replay entry", entries)
	}
	if w := call(a, "GET", path, flow, "", ""); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
}
