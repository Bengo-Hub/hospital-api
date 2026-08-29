// Package authapi is hospital-api's client for auth-api's PUBLIC POST /api/v1/auth/login
// endpoint — used only to re-verify a controlled-substance dispense witness's OWN email+
// password server-side (see internal/modules/pharmacy/witness.go's VerifyWitness). This is
// deliberately NOT built like treasury.Client/inventory.Client's S2S clients: /auth/login takes
// no X-API-Key and no "/api/v1/s2s/{tenant}/..." path prefix, it is a plain public route that
// any client (including a browser) may already call directly — see auth_handler.go's Login and
// its router.go mount at /api/v1/auth/login (outside the S2S group).
package authapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"go.uber.org/zap"
)

// Client calls auth-api's public login endpoint.
type Client struct {
	baseURL string
	enabled bool
	sc      *serviceclient.Client
	log     *zap.Logger
}

// NewClient builds the auth-api login client. Built on the same shared
// github.com/Bengo-Hub/shared-service-client used by treasury.Client/inventory.Client (circuit
// breaker + retry + tracing), per the migration plan's own instruction — not a hand-rolled
// net/http client.
func NewClient(baseURL string, log *zap.Logger) *Client {
	log = log.Named("authapi.client")
	baseURL = strings.TrimRight(baseURL, "/")
	enabled := baseURL != ""
	var sc *serviceclient.Client
	if enabled {
		cfg := serviceclient.DefaultConfig(baseURL, "auth-api", log)
		cfg.Timeout = 10 * time.Second
		sc = serviceclient.New(cfg)
	}
	return &Client{baseURL: baseURL, enabled: enabled, sc: sc, log: log}
}

// Enabled reports whether the client is configured for calls.
func (c *Client) Enabled() bool { return c.enabled }

// LoginRequest mirrors auth-api's loginRequest (auth_handler.go:1189) — JSON tags must match
// verbatim since this is a real request to a real, already-public endpoint.
type LoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TenantSlug string `json:"tenant_slug"`
	TOTPCode   string `json:"totp_code,omitempty"`
}

// LoginResult is the subset of auth-api's /auth/login response this client reads. auth-api
// returns exactly one of two shapes (see auth_handler.go's Login): an MFA challenge
// ({"mfa_required": true, "mfa_method": ..., "user_id": ...}) when the account has TOTP
// enabled and no code was supplied, or a full token grant ({"access_token": ...}) otherwise.
type LoginResult struct {
	AccessToken string `json:"access_token,omitempty"`
	MFARequired bool   `json:"mfa_required,omitempty"`
	MFAMethod   string `json:"mfa_method,omitempty"`
	UserID      string `json:"user_id,omitempty"`
}

// Login calls POST /api/v1/auth/login — a plain public request (no X-API-Key, no S2S path
// prefix). Returns the HTTP status alongside any error so the caller can distinguish "bad
// credentials" (401/403 — a normal, expected outcome for a witness verification attempt) from
// a genuine transport/config failure.
func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginResult, int, error) {
	if !c.enabled {
		return nil, 0, fmt.Errorf("authapi client not configured")
	}
	resp, err := c.sc.Post(ctx, "/api/v1/auth/login", req, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("authapi: login: %w", err)
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, resp.StatusCode, fmt.Errorf("authapi: login: invalid credentials")
	}
	if !resp.IsSuccess() {
		return nil, resp.StatusCode, fmt.Errorf("authapi: login: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out LoginResult
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("authapi: decode login result: %w", err)
	}
	return &out, resp.StatusCode, nil
}
