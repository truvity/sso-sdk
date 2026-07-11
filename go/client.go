// Package sso is the Go SDK for the SSO administration API: every operation
// is scoped to one product, authenticated with the product's machine key.
//
// Minimal usage:
//
//	client, err := sso.New(ctx, sso.Config{
//		Endpoint:  "https://sso-api.example.com",
//		Product:   "acme",
//		Issuer:    "https://login.example.com",
//		ProjectID: "1234567890",
//		Key:       keyJSON, // the machine key JSON issued to your product
//	})
//	user, err := client.CreateUser(ctx, &sso.CreateUserRequest{Email: "jane@example.com"})
package sso

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/zitadel/oidc/v3/pkg/client/profile"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"golang.org/x/oauth2"
)

// Config carries the per-product connection settings. All values are issued
// during product onboarding.
type Config struct {
	// Endpoint is the administration API base URL.
	Endpoint string
	// Product is the registered product identifier; every request is scoped
	// to it and it must match the credentials.
	Product string
	// Issuer is the central identity issuer (https://<domain>).
	Issuer string
	// ProjectID is the central API project id; tokens are minted with its
	// audience so the API can validate them.
	ProjectID string
	// Key is the product's machine key JSON.
	Key []byte
	// HTTPClient optionally overrides the transport (timeouts, proxies,
	// instrumentation). Defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// Client is a product-scoped handle on the administration API. Safe for
// concurrent use.
type Client struct {
	endpoint string
	product  string
	hc       *http.Client
	ts       oauth2.TokenSource
}

// New validates the config and prepares the token source (tokens are minted
// lazily and refreshed automatically).
func New(ctx context.Context, cfg Config) (*Client, error) {
	switch {
	case cfg.Endpoint == "":
		return nil, fmt.Errorf("sso: Endpoint is required")
	case cfg.Product == "":
		return nil, fmt.Errorf("sso: Product is required")
	case cfg.Issuer == "":
		return nil, fmt.Errorf("sso: Issuer is required")
	case cfg.ProjectID == "":
		return nil, fmt.Errorf("sso: ProjectID is required")
	case len(cfg.Key) == 0:
		return nil, fmt.Errorf("sso: Key is required")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	scopes := []string{oidc.ScopeOpenID, fmt.Sprintf("urn:zitadel:iam:org:project:id:%s:aud", cfg.ProjectID)}
	ts, err := profile.NewJWTProfileTokenSourceFromKeyFileData(ctx, cfg.Issuer, cfg.Key, scopes)
	if err != nil {
		return nil, fmt.Errorf("sso: token source: %w", err)
	}
	return &Client{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		product:  cfg.Product,
		hc:       hc,
		ts:       oauth2.ReuseTokenSource(nil, ts),
	}, nil
}

// APIError is a non-2xx response (RFC 9457 problem shape).
type APIError struct {
	Status int           `json:"status"`
	Title  string        `json:"title"`
	Detail string        `json:"detail"`
	Errors []ErrorDetail `json:"errors,omitempty"`
}

// ErrorDetail is one structured error entry.
type ErrorDetail struct {
	Message  string `json:"message"`
	Location string `json:"location"`
	Value    any    `json:"value"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("sso: %d %s: %s", e.Status, e.Title, e.Detail)
}

// ExistingCentralID extracts the already-existing identity id from a
// create-user conflict (use Onboard with it instead). Empty when absent.
func (e *APIError) ExistingCentralID() string {
	if e.Status != http.StatusConflict {
		return ""
	}
	for _, d := range e.Errors {
		if s, ok := d.Value.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// CallOption adjusts one API call.
type CallOption func(*callSettings)

type callSettings struct {
	idempotencyKey string
}

// WithIdempotencyKey pins the Idempotency-Key of a mutating call. Reuse the
// same key when retrying a failed call to make it safe; without this option
// the SDK generates a fresh key per invocation.
func WithIdempotencyKey(key string) CallOption {
	return func(s *callSettings) { s.idempotencyKey = key }
}

// do performs one authenticated JSON call against the product scope.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any, mutation bool, opts ...CallOption) error {
	settings := &callSettings{}
	for _, o := range opts {
		o(settings)
	}
	tok, err := c.ts.Token()
	if err != nil {
		return fmt.Errorf("sso: token: %w", err)
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("sso: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	u := c.endpoint + "/v1/products/" + url.PathEscape(c.product) + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("sso: build request: %w", err)
	}
	tok.SetAuthHeader(req)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutation {
		key := settings.idempotencyKey
		if key == "" {
			key, err = randomKey()
			if err != nil {
				return fmt.Errorf("sso: idempotency key: %w", err)
			}
		}
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("sso: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("sso: read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode, Title: http.StatusText(resp.StatusCode)}
		_ = json.Unmarshal(data, apiErr) // fall back to the bare status on foreign bodies
		return apiErr
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("sso: decode response: %w", err)
		}
	}
	return nil
}

// randomKey returns a UUIDv4 string (no external dependency).
func randomKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
