package databricksplugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTokenServer records token requests and issues numbered tokens.
type fakeTokenServer struct {
	t         *testing.T
	mu        sync.Mutex
	requests  []*http.Request
	forms     []map[string]string
	expiresIn int64
	fail      bool
	server    *httptest.Server
}

func newFakeTokenServer(t *testing.T) *fakeTokenServer {
	f := &fakeTokenServer{t: t, expiresIn: 3600}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oidc/accounts/acc-1/v1/token" {
			t.Errorf("unexpected token path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}

		f.mu.Lock()
		f.requests = append(f.requests, r)
		f.forms = append(f.forms, form)
		n := len(f.forms)
		fail := f.fail
		expiresIn := f.expiresIn
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"access_denied","error_description":"Scopes 'all-apis' are not assigned to the client"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-" + string(rune('0'+n)),
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
		})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeTokenServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forms)
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newProvider(t *testing.T, cfg *DatabricksConfig, clock *fakeClock) *oauthTokenProvider {
	t.Helper()
	p, err := NewTokenProvider(cfg, http.DefaultClient)
	if err != nil {
		t.Fatalf("NewTokenProvider: %v", err)
	}
	oauth, ok := p.(*oauthTokenProvider)
	if !ok {
		t.Fatalf("expected oauth provider, got %T", p)
	}
	oauth.now = clock.Now
	return oauth
}

func TestClientSecretTokenIsFetchedCachedAndRefreshed(t *testing.T) {
	srv := newFakeTokenServer(t)
	clock := &fakeClock{now: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)}
	p := newProvider(t, &DatabricksConfig{
		AccountID:         "acc-1",
		ClientID:          "client-1",
		ClientSecret:      "secret-1",
		Scope:             "billing:read",
		AccountAPIBaseURL: srv.server.URL + "/",
	}, clock)

	tok, err := p.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "access-1" {
		t.Fatalf("unexpected token %q", tok)
	}
	form := srv.forms[0]
	if form["grant_type"] != "client_credentials" || form["scope"] != "billing:read" {
		t.Fatalf("unexpected form %v", form)
	}
	if user, pass, ok := srv.requests[0].BasicAuth(); !ok || user != "client-1" || pass != "secret-1" {
		t.Fatalf("expected basic auth client-1/secret-1, got %q/%q (%v)", user, pass, ok)
	}

	// Still valid 50 minutes later: served from cache.
	clock.now = clock.now.Add(50 * time.Minute)
	if tok, _ = p.Token(); tok != "access-1" || srv.count() != 1 {
		t.Fatalf("expected cached token, got %q after %d requests", tok, srv.count())
	}

	// Within 5 minutes of expiry: renewed.
	clock.now = clock.now.Add(6 * time.Minute)
	if tok, _ = p.Token(); tok != "access-2" || srv.count() != 2 {
		t.Fatalf("expected refreshed token, got %q after %d requests", tok, srv.count())
	}
}

func TestInvalidateForcesNewToken(t *testing.T) {
	srv := newFakeTokenServer(t)
	clock := &fakeClock{now: time.Now()}
	p := newProvider(t, &DatabricksConfig{
		AccountID: "acc-1", ClientID: "c", ClientSecret: "s", Scope: "all-apis",
		AccountAPIBaseURL: srv.server.URL,
	}, clock)

	_, _ = p.Token()
	p.Invalidate()
	if tok, _ := p.Token(); tok != "access-2" {
		t.Fatalf("expected new token after Invalidate, got %q", tok)
	}
}

func TestShortLivedTokensAreNotRefetchedOnEveryCall(t *testing.T) {
	srv := newFakeTokenServer(t)
	srv.expiresIn = 300 // 5 minutes, e.g. a short federated token
	clock := &fakeClock{now: time.Now()}
	p := newProvider(t, &DatabricksConfig{
		AccountID: "acc-1", ClientID: "c", ClientSecret: "s", Scope: "all-apis",
		AccountAPIBaseURL: srv.server.URL,
	}, clock)

	_, _ = p.Token()
	clock.now = clock.now.Add(time.Minute)
	_, _ = p.Token()
	if srv.count() != 1 {
		t.Fatalf("expected 1 token request, got %d", srv.count())
	}
}

func TestFederatedTokenExchangeRereadsTokenFile(t *testing.T) {
	srv := newFakeTokenServer(t)
	clock := &fakeClock{now: time.Now()}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("k8s-jwt-1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := newProvider(t, &DatabricksConfig{
		AccountID:          "acc-1",
		ClientID:           "sp-app-id",
		FederatedTokenFile: tokenFile,
		Scope:              "billing:read",
		AccountAPIBaseURL:  srv.server.URL,
	}, clock)

	if _, err := p.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}
	form := srv.forms[0]
	want := map[string]string{
		"grant_type":         "urn:ietf:params:oauth:grant-type:token-exchange",
		"subject_token":      "k8s-jwt-1",
		"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
		"client_id":          "sp-app-id",
		"scope":              "billing:read",
	}
	for k, v := range want {
		if form[k] != v {
			t.Fatalf("form[%s] = %q, want %q (form %v)", k, form[k], v, form)
		}
	}
	if _, _, ok := srv.requests[0].BasicAuth(); ok {
		t.Fatalf("federated exchange must not send basic auth")
	}

	// Kubernetes rotates the projected token in place; the next exchange
	// must use the new file contents.
	if err := os.WriteFile(tokenFile, []byte("k8s-jwt-2"), 0600); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(2 * time.Hour)
	if _, err := p.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got := srv.forms[1]["subject_token"]; got != "k8s-jwt-2" {
		t.Fatalf("expected rotated subject token, got %q", got)
	}
}

func TestFederatedWithoutClientIDOmitsIt(t *testing.T) {
	srv := newFakeTokenServer(t)
	tokenFile := filepath.Join(t.TempDir(), "token")
	_ = os.WriteFile(tokenFile, []byte("jwt"), 0600)
	p := newProvider(t, &DatabricksConfig{
		AccountID: "acc-1", FederatedTokenFile: tokenFile, Scope: "all-apis",
		AccountAPIBaseURL: srv.server.URL,
	}, &fakeClock{now: time.Now()})

	if _, err := p.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if _, ok := srv.forms[0]["client_id"]; ok {
		t.Fatalf("client_id must be omitted for account-wide federation policies")
	}
}

func TestFederatedTokenFileErrors(t *testing.T) {
	srv := newFakeTokenServer(t)
	empty := filepath.Join(t.TempDir(), "empty")
	_ = os.WriteFile(empty, []byte("  \n"), 0600)

	for name, path := range map[string]string{
		"missing": filepath.Join(t.TempDir(), "does-not-exist"),
		"empty":   empty,
	} {
		p := newProvider(t, &DatabricksConfig{
			AccountID: "acc-1", FederatedTokenFile: path, Scope: "all-apis",
			AccountAPIBaseURL: srv.server.URL,
		}, &fakeClock{now: time.Now()})
		if _, err := p.Token(); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
	if srv.count() != 0 {
		t.Fatalf("no token request expected when the file is unusable")
	}
}

func TestTokenErrorIncludesDatabricksDescription(t *testing.T) {
	srv := newFakeTokenServer(t)
	srv.fail = true
	p := newProvider(t, &DatabricksConfig{
		AccountID: "acc-1", ClientID: "c", ClientSecret: "s", Scope: "all-apis",
		AccountAPIBaseURL: srv.server.URL,
	}, &fakeClock{now: time.Now()})

	_, err := p.Token()
	if err == nil || !strings.Contains(err.Error(), "access_denied") || !strings.Contains(err.Error(), "not assigned") {
		t.Fatalf("expected descriptive error, got %v", err)
	}
}

func TestStaticTokenProvider(t *testing.T) {
	p, err := NewTokenProvider(&DatabricksConfig{AccountID: "acc-1", Token: " tok "}, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := p.Token(); tok != "tok" {
		t.Fatalf("unexpected token %q", tok)
	}
}
