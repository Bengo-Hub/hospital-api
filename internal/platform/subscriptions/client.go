package subscriptions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Config holds configuration for the subscriptions client.
type Config struct {
	ServiceURL     string
	RequestTimeout time.Duration
	APIKey         string
}

// SubscriptionStatus represents the tenant's subscription response from subscriptions-api.
type SubscriptionStatus struct {
	Status string `json:"status"`
}

// IsActive returns true when the subscription status allows service usage.
func (s *SubscriptionStatus) IsActive() bool {
	return s.Status == "ACTIVE" || s.Status == "TRIAL"
}

// Client interacts with the subscriptions service.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient creates a new subscriptions service client.
func NewClient(cfg Config) *Client {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.RequestTimeout}}
}

// IsSubscriptionActive returns true if the tenant has an active subscription.
// Uses the S2S tenant-scoped endpoint so callers don't need to pass a user JWT.
// Fails open (returns true) on network errors to avoid blocking the service on a
// subscriptions-api outage — see the platform's subscription-gate-fail-open rule.
func (c *Client) IsSubscriptionActive(ctx context.Context, tenantID, tenantSlug, bearerToken string) bool {
	url := fmt.Sprintf("%s/api/v1/tenants/%s/subscription", c.cfg.ServiceURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return true // fail open
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	} else if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return true // fail open
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false
	}
	if resp.StatusCode != http.StatusOK {
		return true // fail open
	}
	var sub SubscriptionStatus
	if err := json.NewDecoder(resp.Body).Decode(&sub); err != nil {
		return true // fail open
	}
	return sub.IsActive()
}

// Entitlements is the subscription snapshot for a tenant: active features, plan limits,
// status, and billing mode.
type Entitlements struct {
	Features     []string       `json:"features"`
	Limits       map[string]int `json:"limits"`
	Status       string         `json:"status"`
	BillingMode  string         `json:"billing_mode"`
	PlanCode     string         `json:"plan_code"`
	IsDemoBypass bool           `json:"is_demo_bypass"`
	// FacilityType is the resolved plan's presentation-only facility hint ("chemist" | "clinic" |
	// "facility" | "hospital"), sourced from subscriptions-api's plan Metadata (see
	// subscriptions-api's SubscriptionResult.FacilityType and cmd/seed/plans_hospital.go's
	// afyaTier.facilityType). Empty for tenants on a non-Afya plan family. Additive field — caches
	// into Tenant.metadata (see ent/schema/tenant.go) for hospital-ui's adaptive sidebar.
	FacilityType string `json:"facility_type,omitempty"`
}

// GetEntitlements fetches the tenant's full subscription snapshot (features, limits,
// status, billing_mode) from the S2S endpoint. Returns nil on any error so callers can
// fall back gracefully.
func (c *Client) GetEntitlements(ctx context.Context, tenantID string) *Entitlements {
	if c.cfg.ServiceURL == "" {
		return nil
	}
	url := fmt.Sprintf("%s/api/v1/tenants/%s/subscription", c.cfg.ServiceURL, tenantID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var e Entitlements
	if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
		return nil
	}
	return &e
}
