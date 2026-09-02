// Package inventory is hospital-api's service-to-service client for inventory-api — drug/item
// stock reservation at prescribe time, consumption at dispense time, and the drug-interaction/
// allergy check inventory-api already owns. Built on the shared
// github.com/Bengo-Hub/shared-service-client (circuit breaker + retry + tracing) per the
// migration plan's own instruction, NOT copied from pos-api's hand-rolled net/http client.
package inventory

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Client calls inventory-api's S2S ("/v1/{tenant}/inventory/...") routes, authenticated with the
// shared INTERNAL_SERVICE_KEY (X-API-Key) — the same static key pos-api/ordering-backend use.
type Client struct {
	apiKey  string
	baseURL string
	enabled bool
	sc      *serviceclient.Client
	log     *zap.Logger
}

// NewClient builds the inventory S2S client. When baseURL or apiKey is empty the client is
// disabled and every method returns an error rather than silently no-op'ing — dispensing safety
// (interaction checks, stock consumption) must never be allowed to succeed unconfigured.
func NewClient(baseURL, apiKey string, log *zap.Logger) *Client {
	log = log.Named("inventory.client")
	baseURL = strings.TrimRight(baseURL, "/")
	enabled := baseURL != "" && apiKey != ""
	var sc *serviceclient.Client
	if enabled {
		cfg := serviceclient.DefaultConfig(baseURL, "inventory-api", log)
		cfg.Timeout = 10 * time.Second
		sc = serviceclient.New(cfg)
	}
	return &Client{apiKey: apiKey, baseURL: baseURL, enabled: enabled, sc: sc, log: log}
}

// Enabled reports whether the client is configured for S2S calls.
func (c *Client) Enabled() bool { return c.enabled }

func (c *Client) headers() map[string]string {
	return map[string]string{"X-API-Key": c.apiKey}
}

// ReservationItem is one line to reserve — mirrors inventory-api's stock.ReservationItem.
type ReservationItem struct {
	SKU      string  `json:"sku"`
	Quantity float64 `json:"quantity"`
}

// ReservedItem mirrors inventory-api's stock.ReservedItem.
type ReservedItem struct {
	SKU             string  `json:"sku"`
	RequestedQty    float64 `json:"requested_qty"`
	ReservedQty     float64 `json:"reserved_qty"`
	AvailableQty    float64 `json:"available_qty"`
	IsFullyReserved bool    `json:"is_fully_reserved"`
}

// Reservation mirrors inventory-api's stock.ReservationResponse.
type Reservation struct {
	ID          uuid.UUID      `json:"id"`
	Status      string         `json:"status"`
	Items       []ReservedItem `json:"items"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	ConfirmedAt *time.Time     `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// CreateReservation reserves stock for a prescription's lines before it may be approved —
// mirrors pos-api's pharmacy ApprovePrescription flow (Reserve -> Consume at dispense).
func (c *Client) CreateReservation(ctx context.Context, tenantID, orderID uuid.UUID, items []ReservationItem, idempotencyKey string) (*Reservation, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	body := map[string]any{
		"order_id":        orderID,
		"items":           items,
		"idempotency_key": idempotencyKey,
	}
	resp, err := c.sc.Post(ctx, fmt.Sprintf("/v1/%s/inventory/reservations", tenantID), body, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: create reservation: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: create reservation: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Reservation
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode reservation: %w", err)
	}
	return &out, nil
}

// ReleaseReservation releases a reservation's held stock — called on Reject/Cancel.
func (c *Client) ReleaseReservation(ctx context.Context, tenantID, reservationID uuid.UUID, reason string) error {
	if !c.enabled {
		return fmt.Errorf("inventory client not configured")
	}
	path := fmt.Sprintf("/v1/%s/inventory/reservations/%s/release", tenantID, reservationID)
	resp, err := c.sc.Post(ctx, path, map[string]any{"reason": reason}, c.headers())
	if err != nil {
		return fmt.Errorf("inventory: release reservation: %w", err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("inventory: release reservation: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	return nil
}

// ConsumedLot is one lot's contribution to a dispensed SKU — mirrors inventory-api's
// stock.ConsumedLot, returned since the 2026-08-29 FEFO fix (see inventory-service's
// ConsumeReservation). Empty for tenants not on lot-ordered costing.
type ConsumedLot struct {
	SKU        string     `json:"sku"`
	LotID      uuid.UUID  `json:"lot_id"`
	LotNumber  string     `json:"lot_number,omitempty"`
	ExpiryDate *time.Time `json:"expiry_date,omitempty"`
	Quantity   float64    `json:"quantity"`
}

// ConsumeReservationResult mirrors inventory-api's stock.ConsumeReservationResponse.
type ConsumeReservationResult struct {
	Status       string        `json:"status"`
	LotsConsumed []ConsumedLot `json:"lots_consumed,omitempty"`
}

// ConsumeReservation finalizes a reservation into an actual stock deduction at dispense time,
// drawing down real lots in the tenant's configured FEFO/FIFO/LIFO order — the caller (Prescription
// dispense) should stamp each PrescriptionLine's lot_number/expiry_date from LotsConsumed rather
// than trusting a hand-typed value, per the 2026-08-29 fix that closed this exact gap in pos-api's
// original pharmacy module.
func (c *Client) ConsumeReservation(ctx context.Context, tenantID, reservationID uuid.UUID) (*ConsumeReservationResult, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	path := fmt.Sprintf("/v1/%s/inventory/reservations/%s/consume", tenantID, reservationID)
	resp, err := c.sc.Post(ctx, path, nil, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: consume reservation: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: consume reservation: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out ConsumeReservationResult
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode consume result: %w", err)
	}
	return &out, nil
}

// InteractionFinding mirrors inventory-api's drugInteractionFinding.
type InteractionFinding struct {
	ClassA                 string `json:"class_a"`
	ClassB                 string `json:"class_b"`
	SKUA                   string `json:"sku_a"`
	SKUB                   string `json:"sku_b"`
	Severity               string `json:"severity"`
	Description            string `json:"description,omitempty"`
	ClinicalRecommendation string `json:"clinical_recommendation,omitempty"`
}

// AllergyMatch mirrors inventory-api's allergyMatch.
type AllergyMatch struct {
	SKU              string `json:"sku"`
	ActiveIngredient string `json:"active_ingredient,omitempty"`
	AllergyFlag      string `json:"allergy_flag"`
}

// InteractionCheckResult mirrors inventory-api's checkInteractionsResponse.
type InteractionCheckResult struct {
	Interactions   []InteractionFinding `json:"interactions"`
	AllergyMatches []AllergyMatch       `json:"allergy_matches"`
}

// SearchItem is the subset of inventory-api's items.ItemDTO a drug-search picker needs — mirrors
// the field names docs/integrations.md §1.1 already documented as "read" by this client, even
// though (before this fix) no method here actually called the endpoint that returns them.
type SearchItem struct {
	SKU                   string   `json:"sku"`
	Name                  string   `json:"name"`
	GenericName           string   `json:"generic_name,omitempty"`
	DosageForm            string   `json:"dosage_form,omitempty"`
	Strength              string   `json:"strength,omitempty"`
	DrugClass             string   `json:"drug_class,omitempty"`
	IsControlledSubstance bool     `json:"is_controlled_substance"`
	SellingPrice          *float64 `json:"selling_price,omitempty"`
	Available             *float64 `json:"available,omitempty"`
}

// SearchItems calls inventory-api's real item list/search endpoint (GET /v1/{tenant}/inventory/
// items) scoped to type=DRUG, for a prescription-line autocomplete with a live stock preview —
// previously prescription lines were pure free-text SKU/name/price with no lookup at all. `lean`
// is always set (this caller never needs the image-gallery/preferred-supplier eager loads).
func (c *Client) SearchItems(ctx context.Context, tenantID uuid.UUID, search string) ([]SearchItem, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	q := url.Values{}
	q.Set("type", "DRUG")
	q.Set("lean", "1")
	q.Set("limit", "20")
	if search != "" {
		q.Set("search", search)
	}
	path := fmt.Sprintf("/v1/%s/inventory/items?%s", tenantID, q.Encode())
	resp, err := c.sc.Get(ctx, path, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: search items: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: search items: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		Data []SearchItem `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode search items: %w", err)
	}
	return out.Data, nil
}

// CheckInteractions calls inventory-api's drug-drug/allergy interaction engine. This is a
// first-pass advisory check (an in-house curated rule table, NOT a licensed clinical database —
// see inventory-api's seed_drug_interaction_rules.go) and should be surfaced to the prescriber
// as a warning to review, never silently suppressed.
func (c *Client) CheckInteractions(ctx context.Context, tenantID uuid.UUID, skus []string, allergyFlags []string) (*InteractionCheckResult, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	body := map[string]any{"skus": skus, "allergy_flags": allergyFlags}
	resp, err := c.sc.Post(ctx, fmt.Sprintf("/v1/%s/inventory/items/check-interactions", tenantID), body, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: check interactions: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: check interactions: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out InteractionCheckResult
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode interaction result: %w", err)
	}
	return &out, nil
}
