package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthcheckSucceedsOn200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/readyz" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	out, _, err := execute(t, "healthcheck", "--url", srv.URL+"/readyz", "--json")
	if err != nil {
		t.Fatal(err, out)
	}
	var got map[string]any
	if json.Unmarshal([]byte(out), &got) != nil || got["status"] != float64(200) {
		t.Fatal(out)
	}
}

func TestHealthcheckFailsOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, _, err := execute(t, "healthcheck", "--url", srv.URL+"/readyz"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("503 accepted: %v", err)
	}
}

func TestHealthcheckFailsWhenRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	target := srv.URL + "/readyz"
	srv.Close()
	if _, _, err := execute(t, "healthcheck", "--url", target); err == nil {
		t.Fatal("refused connection accepted")
	}
}

func TestHealthcheckFailsOnTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	start := time.Now()
	_, _, err := execute(t, "healthcheck", "--url", srv.URL+"/readyz", "--timeout", "100ms")
	if err == nil {
		t.Fatal("hung server accepted")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout not enforced: %s", elapsed)
	}
}

func TestHealthcheckNeverFollowsRedirects(t *testing.T) {
	var followed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed.Add(1)
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	if _, _, err := execute(t, "healthcheck", "--url", srv.URL+"/readyz"); err == nil {
		t.Fatal("redirect accepted")
	}
	if followed.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestHealthcheckRejectsNonLoopbackURLs(t *testing.T) {
	for _, target := range []string{"http://192.0.2.1:8787/readyz", "http://example.invalid/readyz", "ftp://127.0.0.1/readyz", "http://user:pw@127.0.0.1/readyz", "readyz"} {
		if _, _, err := execute(t, "healthcheck", "--url", target, "--dry-run"); err == nil {
			t.Fatalf("%s accepted", target)
		}
	}
	for _, target := range []string{defaultHealthURL, "http://localhost:8787/readyz", "http://[::1]:8787/readyz"} {
		if _, _, err := execute(t, "healthcheck", "--url", target, "--dry-run"); err != nil {
			t.Fatalf("%s rejected: %v", target, err)
		}
	}
}
