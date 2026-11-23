package databricksplugin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// refreshBefore renews a token this long before it expires, so a request
	// never starts with a token that is about to expire.
	refreshBefore = 5 * time.Minute
	// defaultTokenLifetime is used when the token endpoint omits expires_in.
	defaultTokenLifetime = time.Hour

	grantTypeClientCredentials = "client_credentials"
	grantTypeTokenExchange     = "urn:ietf:params:oauth:grant-type:token-exchange"
	subjectTokenTypeJWT        = "urn:ietf:params:oauth:token-type:jwt"
)

// HTTPDoer is the subset of *http.Client the token provider needs.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenProvider returns a valid Databricks access token.
type TokenProvider interface {
	// Token returns a cached token, or fetches a new one if the cached token
	// is missing or about to expire.
	Token() (string, error)
	// Invalidate drops the cached token so the next Token call fetches a new
	// one. Called when the API rejects a token before its expected expiry.
	Invalidate()
}

// NewTokenProvider builds the TokenProvider for the auth method selected by the config.
func NewTokenProvider(config *DatabricksConfig, client HTTPDoer) (TokenProvider, error) {
	method, err := config.AuthMethod()
	if err != nil {
		return nil, err
	}
	if method == AuthMethodToken {
		return &staticTokenProvider{token: strings.TrimSpace(config.Token)}, nil
	}
	return &oauthTokenProvider{
		config: config,
		method: method,
		client: client,
		now:    time.Now,
	}, nil
}

type staticTokenProvider struct {
	token string
}

func (s *staticTokenProvider) Token() (string, error) { return s.token, nil }
func (s *staticTokenProvider) Invalidate()            {}

// oauthTokenProvider fetches tokens from the Databricks account OIDC token
// endpoint, using either OAuth M2M (client_secret) or workload identity
// federation (token exchange), and caches them until shortly before expiry.
type oauthTokenProvider struct {
	config *DatabricksConfig
	method string
	client HTTPDoer
	now    func() time.Time

	mu        sync.Mutex
	token     string
	refreshAt time.Time
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (p *oauthTokenProvider) Token() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && p.now().Before(p.refreshAt) {
		return p.token, nil
	}

	token, lifetime, err := p.fetch()
	if err != nil {
		return "", err
	}
	// Renew refreshBefore ahead of expiry, or halfway through for
	// short-lived tokens, so we never refetch on every call.
	early := refreshBefore
	if lifetime/2 < early {
		early = lifetime / 2
	}
	p.token = token
	p.refreshAt = p.now().Add(lifetime - early)
	return p.token, nil
}

func (p *oauthTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = ""
	p.refreshAt = time.Time{}
}

func (p *oauthTokenProvider) tokenURL() string {
	return fmt.Sprintf("%s/oidc/accounts/%s/v1/token",
		strings.TrimRight(p.config.AccountAPIBaseURL, "/"), url.PathEscape(p.config.AccountID))
}

func (p *oauthTokenProvider) fetch() (string, time.Duration, error) {
	form := url.Values{}
	form.Set("scope", p.config.Scope)

	switch p.method {
	case AuthMethodClientSecret:
		form.Set("grant_type", grantTypeClientCredentials)
	case AuthMethodFederated:
		// Re-read on every exchange: platforms such as Kubernetes rotate
		// projected service account tokens in place.
		raw, err := os.ReadFile(p.config.FederatedTokenFile)
		if err != nil {
			return "", 0, fmt.Errorf("reading federated token file %s: %v", p.config.FederatedTokenFile, err)
		}
		subjectToken := strings.TrimSpace(string(raw))
		if subjectToken == "" {
			return "", 0, fmt.Errorf("federated token file %s is empty", p.config.FederatedTokenFile)
		}
		form.Set("grant_type", grantTypeTokenExchange)
		form.Set("subject_token", subjectToken)
		form.Set("subject_token_type", subjectTokenTypeJWT)
		if id := strings.TrimSpace(p.config.ClientID); id != "" {
			form.Set("client_id", id)
		}
	default:
		return "", 0, fmt.Errorf("unsupported auth method %q", p.method)
	}

	req, err := http.NewRequest(http.MethodPost, p.tokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("creating Databricks token request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if p.method == AuthMethodClientSecret {
		req.SetBasicAuth(p.config.ClientID, p.config.ClientSecret)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("requesting Databricks token: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, fmt.Errorf("reading Databricks token response: %v", err)
	}

	var parsed tokenResponse
	jsonErr := json.Unmarshal(body, &parsed)

	if resp.StatusCode != http.StatusOK {
		if jsonErr == nil && parsed.Error != "" {
			return "", 0, fmt.Errorf("Databricks token request failed (%d, %s method): %s: %s",
				resp.StatusCode, p.method, parsed.Error, parsed.ErrorDescription)
		}
		return "", 0, fmt.Errorf("Databricks token request failed (%d, %s method): %s",
			resp.StatusCode, p.method, strings.TrimSpace(string(body)))
	}
	if jsonErr != nil {
		return "", 0, fmt.Errorf("parsing Databricks token response: %v", jsonErr)
	}
	if parsed.AccessToken == "" {
		return "", 0, fmt.Errorf("Databricks token response has no access_token")
	}

	lifetime := defaultTokenLifetime
	if parsed.ExpiresIn > 0 {
		lifetime = time.Duration(parsed.ExpiresIn) * time.Second
	}
	return parsed.AccessToken, lifetime, nil
}
