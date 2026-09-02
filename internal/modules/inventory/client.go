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
// items) scoped to use_case=PHARMACY, for a prescription-line autocomplete with a live stock
// preview — previously prescription lines were pure free-text SKU/name/price with no lookup at
// all. `lean` is always set (this caller never needs the image-gallery/preferred-supplier eager
// loads). Filters on `use_case`, NOT `type`: inventory-api's Item.type enum is
// GOODS|SERVICE|RECIPE|INGREDIENT|VOUCHER|EQUIPMENT — "DRUG" has never been a valid value there
// (confirmed against internal/ent/schema/item.go). A pharmacy item is real inventory GOODS
// classified by the separate `use_case` field (RETAIL|PHARMACY|FOOD_BEVERAGE|...) — this was
// filtering on a value that could never match, silently returning zero results for every tenant
// regardless of whether pharmacy items were actually seeded (codevertex-demo's own 10 real
// PHM-* items, `use_case=PHARMACY`, were sitting in inventory-api the whole time — confirmed live
// 2026-09-02 via a direct curl).
func (c *Client) SearchItems(ctx context.Context, tenantID uuid.UUID, search string) ([]SearchItem, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	q := url.Values{}
	q.Set("use_case", "PHARMACY")
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

// Asset is the read-only subset of inventory-api's Asset fixed-asset register (internal/ent/
// schema/asset.go there) this service needs to surface as "Biomedical Equipment" — reference
// only, hospital-api never owns or writes asset data (see docs/architecture.md's asset-integration
// section). Field names match inventory-api's ent-generated JSON tags.
type Asset struct {
	ID              uuid.UUID  `json:"id"`
	AssetTag        string     `json:"asset_tag"`
	Name            string     `json:"name"`
	CategoryID      *uuid.UUID `json:"category_id,omitempty"`
	SerialNumber    string     `json:"serial_number,omitempty"`
	Model           string     `json:"model,omitempty"`
	Manufacturer    string     `json:"manufacturer,omitempty"`
	Location        string     `json:"location,omitempty"`
	Status          string     `json:"status"`
	Condition       string     `json:"condition,omitempty"`
	WarrantyExpiry  *time.Time `json:"warranty_expiry,omitempty"`
	LastMaintenance *time.Time `json:"last_maintenance,omitempty"`
	NextMaintenance *time.Time `json:"next_maintenance,omitempty"`
}

// AssetMaintenanceRecord is the read-only subset of inventory-api's AssetMaintenance this service
// surfaces alongside an asset's detail view.
type AssetMaintenanceRecord struct {
	ID            uuid.UUID  `json:"id"`
	AssetID       uuid.UUID  `json:"asset_id"`
	ScheduledDate time.Time  `json:"scheduled_date"`
	CompletedDate *time.Time `json:"completed_date,omitempty"`
	Description   string     `json:"description,omitempty"`
	Cost          float64    `json:"cost,omitempty"`
	Status        string     `json:"status,omitempty"`
}

// ListAssets calls inventory-api's fixed-asset register (GET /v1/{tenant}/inventory/assets),
// surfaced in hospital-api as "Biomedical Equipment" — a read-through view, never a local copy
// (see docs/architecture.md). This is the integration the original Sprint 9 plan called for,
// brought forward once a real need (linking equipment to a Bed/TheatreBooking/ICUEpisode)
// appeared during Sprint 6/7's own build.
func (c *Client) ListAssets(ctx context.Context, tenantID uuid.UUID, search string) ([]Asset, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	q := url.Values{}
	q.Set("limit", "100")
	if search != "" {
		q.Set("search", search)
	}
	path := fmt.Sprintf("/v1/%s/inventory/assets?%s", tenantID, q.Encode())
	resp, err := c.sc.Get(ctx, path, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: list assets: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: list assets: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		Data []Asset `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode assets: %w", err)
	}
	return out.Data, nil
}

// GetAsset fetches one asset's detail (GET /v1/{tenant}/inventory/assets/{id}).
func (c *Client) GetAsset(ctx context.Context, tenantID, assetID uuid.UUID) (*Asset, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	path := fmt.Sprintf("/v1/%s/inventory/assets/%s", tenantID, assetID)
	resp, err := c.sc.Get(ctx, path, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: get asset: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: get asset: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var a Asset
	if err := resp.DecodeJSON(&a); err != nil {
		return nil, fmt.Errorf("inventory: decode asset: %w", err)
	}
	return &a, nil
}

// ListAssetMaintenance fetches an asset's maintenance history
// (GET /v1/{tenant}/inventory/assets/{id}/maintenance).
func (c *Client) ListAssetMaintenance(ctx context.Context, tenantID, assetID uuid.UUID) ([]AssetMaintenanceRecord, error) {
	if !c.enabled {
		return nil, fmt.Errorf("inventory client not configured")
	}
	path := fmt.Sprintf("/v1/%s/inventory/assets/%s/maintenance", tenantID, assetID)
	resp, err := c.sc.Get(ctx, path, c.headers())
	if err != nil {
		return nil, fmt.Errorf("inventory: list asset maintenance: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("inventory: list asset maintenance: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out []AssetMaintenanceRecord
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("inventory: decode asset maintenance: %w", err)
	}
	return out, nil
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
