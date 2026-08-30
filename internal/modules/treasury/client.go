// Package treasury is hospital-api's service-to-service client for treasury-api — invoice/
// quotation/credit-note creation, eTIMS sign-at-checkout, payment-intent creation, and the DAWA
// insurance connector (eligibility/coverage/claims). Built on the shared
// github.com/Bengo-Hub/shared-service-client per the migration plan's own instruction, NOT
// copied from pos-api's hand-rolled net/http client. hospital-api stores only the IDs these
// calls return — treasury-api remains the sole owner of every financial document (see
// docs/architecture.md's Data Authority table).
package treasury

import (
	"context"
	"fmt"
	"strings"
	"time"

	serviceclient "github.com/Bengo-Hub/shared-service-client"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Client calls treasury-api's S2S ("/api/v1/s2s/{tenant}/...") routes, authenticated with the
// shared INTERNAL_SERVICE_KEY (X-API-Key).
type Client struct {
	apiKey  string
	baseURL string
	enabled bool
	sc      *serviceclient.Client
	log     *zap.Logger
}

// NewClient builds the treasury S2S client.
func NewClient(baseURL, apiKey string, log *zap.Logger) *Client {
	log = log.Named("treasury.client")
	baseURL = strings.TrimRight(baseURL, "/")
	enabled := baseURL != "" && apiKey != ""
	var sc *serviceclient.Client
	if enabled {
		cfg := serviceclient.DefaultConfig(baseURL, "treasury-api", log)
		cfg.Timeout = 15 * time.Second
		sc = serviceclient.New(cfg)
	}
	return &Client{apiKey: apiKey, baseURL: baseURL, enabled: enabled, sc: sc, log: log}
}

// Enabled reports whether the client is configured for S2S calls.
func (c *Client) Enabled() bool { return c.enabled }

func (c *Client) headers() map[string]string {
	return map[string]string{"X-API-Key": c.apiKey}
}

func (c *Client) s2s(tenantID uuid.UUID, path string) string {
	return fmt.Sprintf("/api/v1/s2s/%s%s", tenantID, path)
}

// ── Invoicing (a BillableCharge's collect action creates one of these) ─────────────────────

// InvoiceLine mirrors treasury-api's invoicing.LineRequest — deliberately loose (no source
// constraint) so a single invoice can mix a consultation-fee line, a lab-fee line, and one or
// more drug-dispense lines, exactly like treasury-api's own docs describe.
type InvoiceLine struct {
	Description string  `json:"description"`
	ItemID      string  `json:"item_id,omitempty"`
	ItemSKU     string  `json:"item_sku,omitempty"`
	ItemType    string  `json:"item_type,omitempty"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	TaxCode     string  `json:"tax_code,omitempty"`
	TaxRate     float64 `json:"tax_rate,omitempty"`
}

// CreateInvoiceRequest mirrors treasury-api's invoicing.CreateInvoiceRequest (the fields
// hospital-api actually uses — treasury-api accepts more, all optional).
type CreateInvoiceRequest struct {
	CustomerID    string        `json:"customer_id,omitempty"`
	CustomerName  string        `json:"customer_name,omitempty"`
	CustomerPhone string        `json:"customer_phone,omitempty"`
	ReferenceID   *uuid.UUID    `json:"reference_id,omitempty"`
	ReferenceType string        `json:"reference_type,omitempty"` // "hospital_encounter"
	OutletID      *uuid.UUID    `json:"outlet_id,omitempty"`
	Lines         []InvoiceLine `json:"lines"`
	Notes         string        `json:"notes,omitempty"`
}

// Invoice is the subset of treasury-api's Invoice response hospital-api needs.
type Invoice struct {
	ID          uuid.UUID `json:"id"`
	InvoiceNo   string    `json:"invoice_no"`
	TotalAmount float64   `json:"total_amount"`
	AmountPaid  float64   `json:"amount_paid"`
	Status      string    `json:"status"`
}

// CreateInvoice aggregates a patient's charges into one treasury invoice. hospital-api stores
// only the returned invoice_id (BillableCharge.treasury_invoice_id).
func (c *Client) CreateInvoice(ctx context.Context, tenantID uuid.UUID, req CreateInvoiceRequest) (*Invoice, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/invoices"), req, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: create invoice: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: create invoice: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Invoice
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode invoice: %w", err)
	}
	return &out, nil
}

// InvoiceByReference finds an already-issued invoice for this encounter, so a checkout retry
// never double-invoices — mirrors subscriptions-api's own renewal-dedup use of this endpoint.
func (c *Client) InvoiceByReference(ctx context.Context, tenantID, referenceID uuid.UUID, referenceType string) (*Invoice, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	path := fmt.Sprintf("/invoices/by-reference?reference_id=%s&reference_type=%s", referenceID, referenceType)
	resp, err := c.sc.Get(ctx, c.s2s(tenantID, path), c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: invoice by reference: %w", err)
	}
	if resp.StatusCode == 404 {
		return nil, nil
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: invoice by reference: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Invoice
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode invoice: %w", err)
	}
	return &out, nil
}

// CreateCreditNote reverses (fully or partially, via lines) an invoice already issued for a
// dispense/encounter — the return/refund path. Empty lines = full reversal.
func (c *Client) CreateCreditNote(ctx context.Context, tenantID, invoiceID uuid.UUID, lines []InvoiceLine) (*Invoice, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	var body any
	if len(lines) > 0 {
		body = map[string]any{"lines": lines}
	}
	path := fmt.Sprintf("/invoices/%s/create-credit-note", invoiceID)
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, path), body, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: create credit note: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: create credit note: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Invoice
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode credit note: %w", err)
	}
	return &out, nil
}

// ── Payments (the actual "Collect Now" action) ──────────────────────────────────────────────

// CreatePaymentIntentRequest mirrors treasury-api's payments.CreateIntentRequest.
type CreatePaymentIntentRequest struct {
	ReferenceID   uuid.UUID  `json:"reference_id"`
	ReferenceType string     `json:"reference_type"` // "hospital_billable_charge" / "hospital_encounter"
	PaymentMethod string     `json:"payment_method"` // mpesa|card|cash|bank_transfer|paystack
	Amount        float64    `json:"amount"`
	Currency      string     `json:"currency"`
	PhoneNumber   string     `json:"phone_number,omitempty"`
	OutletID      *uuid.UUID `json:"outlet_id,omitempty"`
	SourceService string     `json:"source_service"` // "hospital-api"
}

// PaymentIntent is the subset of treasury-api's PaymentIntent response hospital-api needs.
type PaymentIntent struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

// CreateIntent starts (and, for mpesa/paystack, immediately triggers an STK push for) a
// payment — the primitive both a department's direct "Collect Now" action and the Billing
// desk's queue settlement call through.
func (c *Client) CreateIntent(ctx context.Context, tenantID uuid.UUID, req CreatePaymentIntentRequest) (*PaymentIntent, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/payments/intents"), req, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: create payment intent: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: create payment intent: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out PaymentIntent
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode payment intent: %w", err)
	}
	return &out, nil
}

// ── eTIMS (opt-in per tenant; see the hospital_sale Source enum value) ─────────────────────

// ETIMSSaleItem mirrors treasury-api's tax.POSSaleItem.
type ETIMSSaleItem struct {
	SKU              string  `json:"sku"`
	ItemType         string  `json:"item_type"`
	Name             string  `json:"name"`
	Quantity         float64 `json:"quantity"`
	UnitPrice        float64 `json:"unit_price"`
	TotalPrice       float64 `json:"total_price"`
	TaxKraCode       string  `json:"tax_kra_code,omitempty"`
	TaxRate          float64 `json:"tax_rate,omitempty"`
	PriceIncludesTax bool    `json:"price_includes_tax"`
}

// ETIMSSaleTender mirrors treasury-api's tax.POSSaleTender.
type ETIMSSaleTender struct {
	Type   string  `json:"type"`
	Amount float64 `json:"amount"`
}

// SignSaleRequest mirrors treasury-api's tax.POSSaleParams (the fields the S2S endpoint reads).
type SignSaleRequest struct {
	OrderID       uuid.UUID         `json:"order_id"`
	OrderNumber   string            `json:"order_number"`
	TotalAmount   float64           `json:"total_amount"`
	Currency      string            `json:"currency"`
	OutletID      *uuid.UUID        `json:"outlet_id,omitempty"`
	Items         []ETIMSSaleItem   `json:"items"`
	SellingScheme string            `json:"selling_scheme"`
	Tenders       []ETIMSSaleTender `json:"tenders"`
	Source        string            `json:"source"` // always "hospital_sale" for this client
}

// FiscalInfo mirrors treasury-api's eTIMS fiscal evidence response.
type FiscalInfo struct {
	Signed        bool   `json:"signed"`
	InvoiceNumber string `json:"invoice_number,omitempty"`
	QRCodeURL     string `json:"qr_code_url,omitempty"`
}

// SignSaleNow signs a hospital sale at checkout time (synchronous). Returns (nil, nil) when the
// tenant isn't eTIMS-activated — the caller prints an unsigned receipt, exactly as pos-api does.
func (c *Client) SignSaleNow(ctx context.Context, tenantID uuid.UUID, req SignSaleRequest) (*FiscalInfo, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	req.Source = "hospital_sale"
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/etims/sign-pos-sale"), req, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: sign sale: %w", err)
	}
	if resp.StatusCode == 502 {
		return nil, nil // genuine sign failure — caller falls back to the async event path
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: sign sale: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out FiscalInfo
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode fiscal info: %w", err)
	}
	return &out, nil
}

// FiscalEvidence fetches the fiscal evidence for an already-signed hospital sale, for printing
// the "KRA TIMS Details" block on a receipt.
func (c *Client) FiscalEvidence(ctx context.Context, tenantID, sourceID uuid.UUID) (*FiscalInfo, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	path := fmt.Sprintf("/etims-fiscal/hospital_sale/%s", sourceID)
	resp, err := c.sc.Get(ctx, c.s2s(tenantID, path), c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: fiscal evidence: %w", err)
	}
	if resp.StatusCode == 404 {
		return nil, nil // not fiscalised — not an error, tenant may not be eTIMS-activated
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: fiscal evidence: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out FiscalInfo
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode fiscal evidence: %w", err)
	}
	return &out, nil
}

// ── Insurance (the DAWA connector — real, fully built on treasury-api's side) ──────────────

// Coverage mirrors treasury-api's insurance.PatientCoverage (the fields hospital-api reads).
type Coverage struct {
	ID              uuid.UUID `json:"id"`
	ProviderID      uuid.UUID `json:"provider_id"`
	PatientIDNumber string    `json:"patient_id_number"`
	MemberID        string    `json:"member_id"`
	Relationship    string    `json:"relationship,omitempty"`
}

// ListCoverages returns every insurance coverage on file for a patient (by national ID number,
// the same dedup key treasury-api's PatientCoverage already uses).
func (c *Client) ListCoverages(ctx context.Context, tenantID uuid.UUID, patientIDNumber string) ([]Coverage, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	path := fmt.Sprintf("/insurance/coverages?patient_id_number=%s", patientIDNumber)
	resp, err := c.sc.Get(ctx, c.s2s(tenantID, path), c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: list coverages: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: list coverages: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		Data []Coverage `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode coverages: %w", err)
	}
	return out.Data, nil
}

// CreateCoverageRequest registers a patient's insurance membership.
type CreateCoverageRequest struct {
	PatientIDNumber string     `json:"patient_id_number"`
	ProviderID      uuid.UUID  `json:"provider_id"`
	PlanID          *uuid.UUID `json:"plan_id,omitempty"`
	MemberID        string     `json:"member_id"`
	Relationship    string     `json:"relationship,omitempty"`
}

// CreateCoverage registers a new patient insurance coverage — uses the S2S route added
// 2026-08-29 (previously admin-JWT-only), so hospital-api needs no user JWT to do this.
func (c *Client) CreateCoverage(ctx context.Context, tenantID uuid.UUID, req CreateCoverageRequest) (*Coverage, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/insurance/coverages"), req, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: create coverage: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: create coverage: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Coverage
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode coverage: %w", err)
	}
	return &out, nil
}

// EligibilityResult is the raw connector response — shape varies per insurer (SHA/AAR/Jubilee/
// etc., each configured via treasury-api's InsurerConnectorConfig mapping engine), so this is
// deliberately a loose map rather than a rigid struct.
type EligibilityResult map[string]any

// CheckEligibility checks a patient's insurance eligibility against the configured connector for
// the given provider — never blocks checkout if unavailable (per docs/integrations.md §2.4), the
// caller must treat a transport error as "unknown," not "ineligible."
func (c *Client) CheckEligibility(ctx context.Context, tenantID, providerID uuid.UUID, fields map[string]string) (EligibilityResult, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	body := map[string]any{"provider_id": providerID, "fields": fields}
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/insurance/eligibility-check"), body, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: eligibility check: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: eligibility check: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		Data EligibilityResult `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode eligibility result: %w", err)
	}
	return out.Data, nil
}

// SubmitClaimRequest mirrors treasury-api's insurance.SaleClaimInput (the S2S-relevant subset).
type SubmitClaimRequest struct {
	ProviderID     uuid.UUID  `json:"provider_id"`
	CoverageID     *uuid.UUID `json:"coverage_id,omitempty"`
	OutletID       *uuid.UUID `json:"outlet_id,omitempty"`
	OrderID        *uuid.UUID `json:"order_id,omitempty"`
	PrescriptionID *uuid.UUID `json:"prescription_id,omitempty"`
	Amount         float64    `json:"amount"`
}

// Claim mirrors treasury-api's insurance.InsuranceClaim (the fields hospital-api reads).
type Claim struct {
	ID             uuid.UUID `json:"id"`
	Status         string    `json:"status"`
	ClaimReference string    `json:"claim_reference,omitempty"` // SHA's mediator_id once submitted
}

// SubmitClaim submits an insurance claim for a dispensed/billed item — idempotent on order_id
// treasury-api-side. Best-effort against the external payer; a transport failure still leaves
// the claim recorded locally in treasury-api for later retry/poll.
func (c *Client) SubmitClaim(ctx context.Context, tenantID uuid.UUID, req SubmitClaimRequest) (*Claim, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	resp, err := c.sc.Post(ctx, c.s2s(tenantID, "/insurance/claims"), req, c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: submit claim: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: submit claim: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Claim
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode claim: %w", err)
	}
	return &out, nil
}

// PollClaimStatus polls a submitted claim's async adjudication status (SHA's mediator_id
// pattern) — uses the S2S route added 2026-08-29 (previously admin-JWT-only).
func (c *Client) PollClaimStatus(ctx context.Context, tenantID, claimID uuid.UUID) (*Claim, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	path := fmt.Sprintf("/insurance/claims/%s/status", claimID)
	resp, err := c.sc.Get(ctx, c.s2s(tenantID, path), c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: poll claim status: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: poll claim status: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out Claim
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode claim: %w", err)
	}
	return &out, nil
}

// Provider mirrors treasury-api's InsuranceProvider (the fields hospital-api's picker needs).
type Provider struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	IsActive bool      `json:"is_active"`
}

// ListProviders lists a tenant's configured insurance providers — the picker source for the
// eligibility-check/claim-submission UI. Uses the S2S route added 2026-08-30 (previously
// admin-JWT-only, same reasoning as PollClaimStatus's 2026-08-29 fix above).
func (c *Client) ListProviders(ctx context.Context, tenantID uuid.UUID) ([]Provider, error) {
	if !c.enabled {
		return nil, fmt.Errorf("treasury client not configured")
	}
	resp, err := c.sc.Get(ctx, c.s2s(tenantID, "/insurance/providers"), c.headers())
	if err != nil {
		return nil, fmt.Errorf("treasury: list providers: %w", err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("treasury: list providers: status %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		Data []Provider `json:"data"`
	}
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, fmt.Errorf("treasury: decode providers: %w", err)
	}
	return out.Data, nil
}
