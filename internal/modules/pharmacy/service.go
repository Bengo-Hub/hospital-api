// Package pharmacy implements Sprint 4: prescription creation, lifecycle (approve/lock/reject/
// cancel), dispensing, drug-interaction checks, and the controlled-substance dual-witness
// register — migrated in meaning from pos-api's pharmacy module (see
// migration-pos-pharmacy.md), the core target of this migration. Stock reservation/consumption
// and the interaction engine are inventory-api's (internal/modules/inventory.Client, built on
// shared/service-client per the migration plan's own instruction); dispense charges post
// through billing.Service, never a direct treasury call.
package pharmacy

import (
	"context"
	"fmt"

	authclient "github.com/Bengo-Hub/shared-auth-client"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/controlledsubstancelog"
	"github.com/bengobox/hospital-service/internal/ent/prescription"
	"github.com/bengobox/hospital-service/internal/ent/prescriptionline"
	"github.com/bengobox/hospital-service/internal/ent/walkinsale"
	"github.com/bengobox/hospital-service/internal/modules/authapi"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/inventory"
	"github.com/bengobox/hospital-service/internal/modules/rbac"
	"github.com/bengobox/hospital-service/internal/modules/sequence"
)

// Service implements pharmacy business logic.
type Service struct {
	client    *ent.Client
	inventory *inventory.Client
	billing   *billing.Service
	log       *zap.Logger

	// Controlled-substance dual-witness re-authentication (see witness.go / VerifyWitness).
	// authAPI verifies the witness's own email+password against auth-api's public login;
	// authValidator (hospital-api's own JWKS validator, the same instance RequireAuth uses)
	// validates the token that login returns; rbac checks the resolved witness holds
	// pharmacy-dispensing permission; witnessSecret signs/verifies the short-lived internal
	// witness token Dispense consumes. All four are nil-safe: VerifyWitness rejects (never
	// silently no-ops) if any is missing.
	authAPI       *authapi.Client
	authValidator *authclient.Validator
	rbac          *rbac.Service
	witnessSecret []byte
}

// NewService creates a new pharmacy service.
func NewService(client *ent.Client, inventoryClient *inventory.Client, billingSvc *billing.Service, log *zap.Logger,
	authAPIClient *authapi.Client, authValidator *authclient.Validator, rbacService *rbac.Service, witnessSecret []byte,
) *Service {
	return &Service{
		client: client, inventory: inventoryClient, billing: billingSvc, log: log.Named("pharmacy.service"),
		authAPI: authAPIClient, authValidator: authValidator, rbac: rbacService, witnessSecret: witnessSecret,
	}
}

// PrescriptionLineInput is one requested drug line.
type PrescriptionLineInput struct {
	InventoryItemSKU   string
	DrugName           string
	Dosage             string
	Form               string
	Instructions       string
	QuantityPrescribed float64
	UnitPrice          float64
}

// CreatePrescriptionRequest is the input to CreatePrescription.
type CreatePrescriptionRequest struct {
	OutletID             uuid.UUID
	PatientID            *uuid.UUID
	VisitID              *uuid.UUID
	ExaminationID        *uuid.UUID
	ExternalFacilityName string
	PrescriberName       string
	PrescriberLicense    string
	PatientName          string
	PatientIDNumber      string
	AllergyFlags         []string
	Lines                []PrescriptionLineInput
}

// CreatePrescription creates a Prescription with its lines, then runs a drug-interaction/
// allergy check (inventory-api's engine — a first-pass advisory check, not a licensed clinical
// database, see internal/modules/inventory.CheckInteractions). Findings flag the prescription
// for pharmacist review rather than silently proceeding or silently blocking.
func (s *Service) CreatePrescription(ctx context.Context, tenantID uuid.UUID, req CreatePrescriptionRequest) (*ent.Prescription, error) {
	if len(req.Lines) == 0 {
		return nil, fmt.Errorf("pharmacy: at least one drug line is required")
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rxNo, err := sequence.Next(ctx, tx, tenantID, "prescription_number", "RX", 6)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: allocate prescription_number: %w", err)
	}

	create := tx.Prescription.Create().
		SetTenantID(tenantID).
		SetOutletID(req.OutletID).
		SetPrescriptionNumber(rxNo).
		SetExternalFacilityName(req.ExternalFacilityName).
		SetPrescriberName(req.PrescriberName).
		SetPrescriberLicense(req.PrescriberLicense).
		SetPatientName(req.PatientName).
		SetPatientIDNumber(req.PatientIDNumber).
		SetMetadata(map[string]any{"allergy_flags": req.AllergyFlags})
	if req.PatientID != nil {
		create = create.SetPatientID(*req.PatientID)
	}
	if req.VisitID != nil {
		create = create.SetVisitID(*req.VisitID)
	}
	if req.ExaminationID != nil {
		create = create.SetExaminationID(*req.ExaminationID)
	}
	rx, err := create.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: create prescription: %w", err)
	}

	skus := make([]string, 0, len(req.Lines))
	for _, l := range req.Lines {
		if _, lerr := tx.PrescriptionLine.Create().
			SetTenantID(tenantID).
			SetPrescriptionID(rx.ID).
			SetInventoryItemSku(l.InventoryItemSKU).
			SetDrugName(l.DrugName).
			SetDosage(l.Dosage).
			SetForm(l.Form).
			SetInstructions(l.Instructions).
			SetQuantityPrescribed(l.QuantityPrescribed).
			SetUnitPrice(l.UnitPrice).
			Save(ctx); lerr != nil {
			err = lerr
			return nil, fmt.Errorf("pharmacy: create prescription line: %w", err)
		}
		if l.InventoryItemSKU != "" {
			skus = append(skus, l.InventoryItemSKU)
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("pharmacy: commit prescription: %w", err)
	}

	if len(skus) > 0 && s.inventory.Enabled() {
		s.runInteractionCheck(ctx, tenantID, rx.ID, skus, req.AllergyFlags)
	}
	return s.GetPrescription(ctx, tenantID, rx.ID)
}

// runInteractionCheck calls inventory-api's interaction engine and, on any finding, flags the
// prescription for review. Best-effort: a transport failure is logged, not fatal — dispensing
// safety checks should degrade to "needs manual review," never silently vanish.
//
// Severity tiering: inventory-api's DrugInteractionRule.Severity (minor/moderate/major/
// contraindicated — see InteractionFinding.Severity) is the only severity signal this check
// actually receives; AllergyMatch carries none. A major/contraindicated drug-drug interaction
// routes to the stricter StatusPharmacistReview tier rather than the plain StatusFlagged tier
// used for minor/moderate findings and allergy-only matches — so a genuinely contraindicated
// combination surfaces as more serious than a routine flag, per hospital-ui's own (previously
// unreachable) pharmacist_review messaging in pharmacy/[id]/page.tsx.
func (s *Service) runInteractionCheck(ctx context.Context, tenantID, rxID uuid.UUID, skus, allergyFlags []string) {
	if _, _, err := s.performInteractionCheck(ctx, tenantID, rxID, skus, allergyFlags, true); err != nil {
		s.log.Warn("interaction check failed", zap.String("prescription_id", rxID.String()), zap.Error(err))
	}
}

// performInteractionCheck is the shared core of both the fire-and-forget check at prescription
// creation (runInteractionCheck) and the on-demand RecheckInteractions (2026-08-30, e.g. a
// late-disclosed allergy after the initial check). Returns the saved check record and whether
// this call flagged/re-flagged the prescription for review. allowStatusChange gates the
// flagging side-effect: creation-time always allows it (nothing to protect yet); a re-check
// after the prescription has moved past review (approved/locked/dispensed/etc.) still records
// the check for audit but the caller decides whether re-flagging is appropriate for that status.
func (s *Service) performInteractionCheck(ctx context.Context, tenantID, rxID uuid.UUID, skus, allergyFlags []string, allowStatusChange bool) (*ent.DrugInteractionCheck, bool, error) {
	result, err := s.inventory.CheckInteractions(ctx, tenantID, skus, allergyFlags)
	if err != nil {
		return nil, false, fmt.Errorf("pharmacy: check interactions: %w", err)
	}
	outcome := "clear"
	if len(result.Interactions) > 0 || len(result.AllergyMatches) > 0 {
		outcome = "interactions_found"
		if len(result.Interactions) == 0 {
			outcome = "allergy_match"
		}
	}
	highSeverity := false
	for _, f := range result.Interactions {
		if f.Severity == "major" || f.Severity == "contraindicated" {
			highSeverity = true
			break
		}
	}
	check, cerr := s.client.DrugInteractionCheck.Create().
		SetTenantID(tenantID).
		SetPrescriptionID(rxID).
		SetDrugSkus(skus).
		SetResult(outcome).
		SetDetails(map[string]any{
			"interactions":    result.Interactions,
			"allergy_matches": result.AllergyMatches,
		}).
		Save(ctx)
	if cerr != nil {
		return nil, false, fmt.Errorf("pharmacy: save interaction check: %w", cerr)
	}
	flagged := false
	if outcome != "clear" && allowStatusChange {
		rx, gerr := s.client.Prescription.Get(ctx, rxID)
		if gerr == nil {
			meta := rx.Metadata
			if meta == nil {
				meta = map[string]any{}
			}
			meta["interaction_check_id"] = check.ID.String()
			status := prescription.StatusFlagged
			if highSeverity {
				status = prescription.StatusPharmacistReview
			}
			if _, uerr := s.client.Prescription.UpdateOneID(rxID).
				SetStatus(status).
				SetMetadata(meta).
				Save(ctx); uerr == nil {
				flagged = true
			}
		}
	}
	return check, flagged, nil
}

// RecheckInteractions re-runs the drug-interaction/allergy check against a prescription's
// CURRENT lines — for a late-disclosed allergy or any reason the pharmacist wants a fresh check
// before dispensing (previously impossible: hospital-api only ever checked once, at creation).
// extraAllergyFlags are merged with whatever was captured at creation time. Re-flags the
// prescription for review (same severity tiering as the original check) ONLY if it is still in
// a pre-dispense state (pending/flagged/pharmacist_review/approved/locked) — a dispensed,
// rejected, or cancelled prescription can't be un-dispensed by a check result, so the finding is
// still recorded for the audit trail but does not change the prescription's status.
func (s *Service) RecheckInteractions(ctx context.Context, tenantID, rxID uuid.UUID, extraAllergyFlags []string) (*ent.DrugInteractionCheck, *ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	skus := make([]string, 0, len(rx.Edges.Lines))
	for _, l := range rx.Edges.Lines {
		if l.InventoryItemSku != "" {
			skus = append(skus, l.InventoryItemSku)
		}
	}
	if len(skus) == 0 {
		return nil, nil, fmt.Errorf("pharmacy: prescription has no inventory-linked lines to check")
	}
	if !s.inventory.Enabled() {
		return nil, nil, fmt.Errorf("pharmacy: inventory client not configured")
	}

	allergyFlags := extraAllergyFlags
	if existing, ok := rx.Metadata["allergy_flags"].([]any); ok {
		seen := make(map[string]bool, len(existing)+len(extraAllergyFlags))
		merged := make([]string, 0, len(existing)+len(extraAllergyFlags))
		for _, a := range existing {
			if s, ok := a.(string); ok && !seen[s] {
				seen[s] = true
				merged = append(merged, s)
			}
		}
		for _, a := range extraAllergyFlags {
			if !seen[a] {
				seen[a] = true
				merged = append(merged, a)
			}
		}
		allergyFlags = merged
	}

	canReflag := rx.Status == prescription.StatusPending || rx.Status == prescription.StatusFlagged ||
		rx.Status == prescription.StatusPharmacistReview || rx.Status == prescription.StatusApproved ||
		rx.Status == prescription.StatusLocked
	check, _, err := s.performInteractionCheck(ctx, tenantID, rxID, skus, allergyFlags, canReflag)
	if err != nil {
		return nil, nil, err
	}
	updated, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return check, rx, nil // check succeeded; re-fetch is best-effort
	}
	return check, updated, nil
}

// GetPrescription fetches a prescription with its lines and any walk-in sale(s) it generated,
// tenant-scoped.
func (s *Service) GetPrescription(ctx context.Context, tenantID, id uuid.UUID) (*ent.Prescription, error) {
	return s.client.Prescription.Query().
		Where(prescription.ID(id), prescription.TenantID(tenantID)).
		WithLines().
		WithWalkInSales(func(q *ent.WalkInSaleQuery) { q.Order(ent.Desc(walkinsale.FieldCreatedAt)) }).
		Only(ctx)
}

// ListPrescriptions lists prescriptions for a tenant, optionally filtered by status.
func (s *Service) ListPrescriptions(ctx context.Context, tenantID uuid.UUID, status string) ([]*ent.Prescription, error) {
	q := s.client.Prescription.Query().Where(prescription.TenantID(tenantID))
	if status != "" {
		q = q.Where(prescription.StatusEQ(prescription.Status(status)))
	}
	return q.Order(ent.Desc(prescription.FieldCreatedAt)).Limit(200).All(ctx)
}

// ApprovePrescription reserves stock for every SKU-bearing line via inventory-api, then marks
// the prescription approved. An explicit override reason is required to approve a prescription
// flagged for review (StatusFlagged: a minor/moderate interaction or allergy finding, or
// StatusPharmacistReview: a major/contraindicated interaction — see runInteractionCheck) —
// never a silent bypass, and this applies to BOTH tiers: pharmacist_review is the more serious
// of the two, so it must never require less to approve than flagged does.
func (s *Service) ApprovePrescription(ctx context.Context, tenantID, rxID, approvedBy uuid.UUID, overrideReason string) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if (rx.Status == prescription.StatusFlagged || rx.Status == prescription.StatusPharmacistReview) && overrideReason == "" {
		return nil, fmt.Errorf("pharmacy: prescription flagged for review — an override reason is required to approve")
	}
	if rx.Status != prescription.StatusPending && rx.Status != prescription.StatusFlagged && rx.Status != prescription.StatusPharmacistReview {
		return nil, fmt.Errorf("pharmacy: prescription is not awaiting approval (status=%s)", rx.Status)
	}

	items := make([]inventory.ReservationItem, 0, len(rx.Edges.Lines))
	for _, l := range rx.Edges.Lines {
		if l.InventoryItemSku != "" {
			items = append(items, inventory.ReservationItem{SKU: l.InventoryItemSku, Quantity: l.QuantityPrescribed})
		}
	}

	meta := rx.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["approved_by"] = approvedBy.String()
	if overrideReason != "" {
		meta["approval_override_reason"] = overrideReason
	}

	if len(items) > 0 && s.inventory.Enabled() {
		resv, rerr := s.inventory.CreateReservation(ctx, tenantID, rxID, items, rxID.String())
		if rerr != nil {
			return nil, fmt.Errorf("pharmacy: reserve stock: %w", rerr)
		}
		meta["reservation_id"] = resv.ID.String()
	}

	return s.client.Prescription.UpdateOneID(rxID).
		SetStatus(prescription.StatusApproved).
		SetMetadata(meta).
		Save(ctx)
}

// LockPrescription locks an approved prescription so its lines can no longer be edited before
// dispense — an optional stage some outlets require between approval and dispensing.
func (s *Service) LockPrescription(ctx context.Context, tenantID, rxID uuid.UUID) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if rx.Status != prescription.StatusApproved {
		return nil, fmt.Errorf("pharmacy: only an approved prescription can be locked (status=%s)", rx.Status)
	}
	return s.client.Prescription.UpdateOneID(rxID).SetStatus(prescription.StatusLocked).Save(ctx)
}

// releaseReservation is shared by Reject/Cancel.
func (s *Service) releaseReservation(ctx context.Context, tenantID uuid.UUID, rx *ent.Prescription, reason string) {
	if rx.Metadata == nil {
		return
	}
	resvID, ok := rx.Metadata["reservation_id"].(string)
	if !ok || resvID == "" || !s.inventory.Enabled() {
		return
	}
	id, err := uuid.Parse(resvID)
	if err != nil {
		return
	}
	if err := s.inventory.ReleaseReservation(ctx, tenantID, id, reason); err != nil {
		s.log.Warn("release reservation failed", zap.String("prescription_id", rx.ID.String()), zap.Error(err))
	}
}

// RejectPrescription rejects a prescription (e.g. pharmacist declines to fill it), releasing
// any held stock reservation.
func (s *Service) RejectPrescription(ctx context.Context, tenantID, rxID uuid.UUID, reason string) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	s.releaseReservation(ctx, tenantID, rx, reason)
	meta := rx.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["cancel_reason"] = reason
	return s.client.Prescription.UpdateOneID(rxID).SetStatus(prescription.StatusRejected).SetMetadata(meta).Save(ctx)
}

// CancelPrescription cancels a prescription at any pre-dispense stage, releasing any held
// reservation.
func (s *Service) CancelPrescription(ctx context.Context, tenantID, rxID uuid.UUID, reason string) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if rx.Status == prescription.StatusDispensed {
		return nil, fmt.Errorf("pharmacy: a fully dispensed prescription cannot be cancelled")
	}
	s.releaseReservation(ctx, tenantID, rx, reason)
	meta := rx.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	meta["cancel_reason"] = reason
	return s.client.Prescription.UpdateOneID(rxID).SetStatus(prescription.StatusCancelled).SetMetadata(meta).Save(ctx)
}

// DispenseLineInput is one line's dispense action.
type DispenseLineInput struct {
	LineID             uuid.UUID
	QuantityToDispense float64
	// RequiresWitness/WitnessToken drive the controlled-substance dual-witness register — the
	// dispensing UI determines RequiresWitness from inventory-api's
	// Item.controlled_substance_schedule (fetched separately). WitnessToken is the short-lived
	// token minted by VerifyWitness (witness.go) after the witness re-authenticated their OWN
	// credentials — it is NEVER a client-supplied staff UUID. A raw witness UUID from the
	// request body was the original vulnerability here (any dispensing user could name ANY
	// staff member with zero verification); that path is fully closed, not left as a fallback.
	RequiresWitness bool
	WitnessToken    *string
}

// DispenseRequest is the input to Dispense.
type DispenseRequest struct {
	DispensedBy     uuid.UUID
	OutletID        uuid.UUID
	PatientName     string
	PatientIDNumber string
	Lines           []DispenseLineInput
}

// Dispense finalizes a reservation into an actual stock deduction via inventory-api's fixed
// FEFO-aware ConsumeReservation, stamps each line's lot_number/expiry_date from the real lots
// drawn (not hand-typed), posts a BillableCharge per line, and writes a ControlledSubstanceLog
// entry for any line whose witness was supplied. Requires the prescription to be approved or
// locked.
func (s *Service) Dispense(ctx context.Context, tenantID, rxID uuid.UUID, req DispenseRequest) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if rx.Status != prescription.StatusApproved && rx.Status != prescription.StatusLocked {
		return nil, fmt.Errorf("pharmacy: prescription must be approved or locked before dispensing (status=%s)", rx.Status)
	}
	// Every witness-requiring line must carry a valid, ALREADY-VERIFIED witness token (minted
	// by VerifyWitness, see witness.go) — never a client-supplied staff UUID. Verified up front,
	// before any reservation consumption or DB write, exactly like the original nil-check did.
	witnessUserIDs := make([]uuid.UUID, len(req.Lines))
	for i, dl := range req.Lines {
		if !dl.RequiresWitness {
			continue
		}
		if dl.WitnessToken == nil || *dl.WitnessToken == "" {
			return nil, fmt.Errorf("pharmacy: a verified witness is required to dispense this controlled substance")
		}
		wid, ok := verifyWitnessToken(*dl.WitnessToken, tenantID, s.witnessSecret)
		if !ok {
			return nil, fmt.Errorf("pharmacy: witness verification token is invalid or expired")
		}
		witnessUserIDs[i] = wid
	}

	var lotsBySKU map[string][]inventory.ConsumedLot
	if resvID, ok := rx.Metadata["reservation_id"].(string); ok && resvID != "" && s.inventory.Enabled() {
		id, perr := uuid.Parse(resvID)
		if perr != nil {
			return nil, fmt.Errorf("pharmacy: invalid reservation_id in metadata")
		}
		result, cerr := s.inventory.ConsumeReservation(ctx, tenantID, id)
		if cerr != nil {
			return nil, fmt.Errorf("pharmacy: consume reservation: %w", cerr)
		}
		lotsBySKU = map[string][]inventory.ConsumedLot{}
		for _, lot := range result.LotsConsumed {
			lotsBySKU[lot.SKU] = append(lotsBySKU[lot.SKU], lot)
		}
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	allDispensed := true
	var walkInLines []map[string]any
	var walkInTotal float64
	for i, dl := range req.Lines {
		line, lerr := tx.PrescriptionLine.Get(ctx, dl.LineID)
		if lerr != nil {
			err = lerr
			return nil, fmt.Errorf("pharmacy: line not found: %w", lerr)
		}
		upd := tx.PrescriptionLine.UpdateOneID(dl.LineID).
			AddQuantityDispensed(dl.QuantityToDispense)
		if lots, ok := lotsBySKU[line.InventoryItemSku]; ok && len(lots) > 0 {
			l := lots[0]
			upd = upd.SetLotNumber(l.LotNumber)
			if l.ExpiryDate != nil {
				upd = upd.SetExpiryDate(*l.ExpiryDate)
			}
		}
		newQtyDispensed := line.QuantityDispensed + dl.QuantityToDispense
		lineStatus := prescriptionline.StatusPartiallyDispensed
		if newQtyDispensed >= line.QuantityPrescribed {
			lineStatus = prescriptionline.StatusDispensed
		} else {
			allDispensed = false
		}
		upd = upd.SetStatus(lineStatus)
		if _, uerr := upd.Save(ctx); uerr != nil {
			err = uerr
			return nil, fmt.Errorf("pharmacy: update line: %w", uerr)
		}

		if dl.RequiresWitness {
			logCreate := tx.ControlledSubstanceLog.Create().
				SetTenantID(tenantID).
				SetOutletID(req.OutletID).
				SetPrescriptionID(rxID).
				SetItemSku(line.InventoryItemSku).
				SetItemName(line.DrugName).
				SetQuantityDispensed(dl.QuantityToDispense).
				SetDispensedBy(req.DispensedBy).
				SetPatientName(req.PatientName).
				SetPatientIDNumber(req.PatientIDNumber).
				SetLotNumber(line.LotNumber).
				// witness_staff_id is intentionally NOT read from any client-supplied field —
				// it comes ONLY from the VERIFIED witness token checked above (witnessUserIDs[i]).
				// Trusting the request body would let a client claim any witness without
				// actually authorizing them.
				SetWitnessStaffID(witnessUserIDs[i])
			if line.ExpiryDate != nil {
				logCreate = logCreate.SetLotExpiryDate(*line.ExpiryDate)
			}
			if _, lerr := logCreate.Save(ctx); lerr != nil {
				err = lerr
				return nil, fmt.Errorf("pharmacy: create controlled substance log: %w", lerr)
			}
		}

		if line.UnitPrice > 0 {
			lineTotal := line.UnitPrice * dl.QuantityToDispense
			if rx.PatientID != nil && rx.VisitID != nil {
				if _, cerr := s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
					PatientID:     *rx.PatientID,
					VisitID:       *rx.VisitID,
					SourceModule:  "pharmacy",
					SourceRefID:   &line.ID,
					Description:   "Drug dispensed: " + line.DrugName,
					Amount:        lineTotal,
					CreatedByUser: req.DispensedBy,
				}); cerr != nil {
					err = cerr
					return nil, fmt.Errorf("pharmacy: post charge for %s: %w", line.DrugName, cerr)
				}
			} else {
				// No patient/visit (a Chemist walk-in, or a non-chemist tenant's own "— Walk-in
				// (no patient record) —" prescription option) — the patient ledger doesn't apply
				// here (see billing.CreateWalkInSale's doc comment). Accumulate into one
				// WalkInSale for the whole dispense action rather than dropping the charge
				// silently, which is the exact bug this fixes.
				walkInLines = append(walkInLines, map[string]any{
					"drug_name":  line.DrugName,
					"sku":        line.InventoryItemSku,
					"quantity":   dl.QuantityToDispense,
					"unit_price": line.UnitPrice,
					"line_total": lineTotal,
				})
				walkInTotal += lineTotal
			}
		}
	}

	if walkInTotal > 0 {
		if _, werr := s.billing.CreateWalkInSale(ctx, tx, tenantID, billing.CreateWalkInSaleRequest{
			OutletID:           req.OutletID,
			PrescriptionID:     rxID,
			PrescriptionNumber: rx.PrescriptionNumber,
			PatientName:        rx.PatientName,
			Amount:             walkInTotal,
			LineItems:          walkInLines,
			CreatedByUser:      req.DispensedBy,
		}); werr != nil {
			err = werr
			return nil, fmt.Errorf("pharmacy: create walk-in sale: %w", werr)
		}
	}

	rxStatus := prescription.StatusPartiallyDispensed
	if allDispensed {
		rxStatus = prescription.StatusDispensed
	}
	updated, err := tx.Prescription.UpdateOneID(rxID).
		SetStatus(rxStatus).
		SetDispensedBy(req.DispensedBy).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: update prescription status: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("pharmacy: commit dispense: %w", err)
	}
	return updated, nil
}

// SubmitInsuranceClaimRequest is the input to SubmitInsuranceClaim.
type SubmitInsuranceClaimRequest struct {
	ProviderID uuid.UUID
	CoverageID *uuid.UUID
	OutletID   *uuid.UUID
	// LineIDs optionally restricts the claim to specific PrescriptionLine charges (e.g. the
	// patient's cover only reimburses some of the dispensed drugs); empty = every pending
	// pharmacy charge for this prescription.
	LineIDs []uuid.UUID
}

// SubmitInsuranceClaim is the insurance-settlement counterpart to a cash
// POST .../billing/charges/{id}/collect for a dispensed line: submits a treasury-api claim
// (tagged with this prescription's ID, per treasury.SubmitClaimRequest.PrescriptionID) covering
// the dispensed lines' still-pending charges. Dispensing itself already happened via Dispense —
// this only settles how the resulting charge gets paid, exactly like the cash collect action
// does. See billing.Service.SubmitInsuranceClaim for the accept/pending-adjudication split.
func (s *Service) SubmitInsuranceClaim(ctx context.Context, tenantID, rxID uuid.UUID, req SubmitInsuranceClaimRequest) (*ent.Prescription, *billing.InsuranceClaimResult, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if rx.PatientID == nil || rx.VisitID == nil {
		return nil, nil, fmt.Errorf("pharmacy: prescription has no linked patient/visit account to claim against")
	}
	acct, _, err := s.billing.GetAccountByVisit(ctx, tenantID, *rx.VisitID)
	if err != nil {
		return nil, nil, fmt.Errorf("pharmacy: account not found: %w", err)
	}

	var wanted map[uuid.UUID]bool
	if len(req.LineIDs) > 0 {
		wanted = make(map[uuid.UUID]bool, len(req.LineIDs))
		for _, id := range req.LineIDs {
			wanted[id] = true
		}
	}
	chargeIDs := make([]uuid.UUID, 0, len(rx.Edges.Lines))
	for _, l := range rx.Edges.Lines {
		if wanted != nil && !wanted[l.ID] {
			continue
		}
		charge, cerr := s.client.BillableCharge.Query().
			Where(billablecharge.TenantID(tenantID), billablecharge.SourceReferenceID(l.ID),
				billablecharge.StatusEQ(billablecharge.StatusPending)).
			Only(ctx)
		if cerr == nil && charge != nil {
			chargeIDs = append(chargeIDs, charge.ID)
		}
	}
	if len(chargeIDs) == 0 {
		return rx, nil, fmt.Errorf("pharmacy: no pending charges to claim for this prescription")
	}

	result, err := s.billing.SubmitInsuranceClaim(ctx, tenantID, acct.ID, billing.SubmitInsuranceClaimRequest{
		ProviderID:     req.ProviderID,
		CoverageID:     req.CoverageID,
		OutletID:       req.OutletID,
		PrescriptionID: &rxID,
		ChargeIDs:      chargeIDs,
	})
	if err != nil {
		return rx, nil, err
	}
	return rx, result, nil
}

// ── WalkInSale delegates (Chemist-tier ledgerless checkout — see billing.Service's own doc ────
// comment for the full design) — thin pass-throughs, same pattern as SubmitInsuranceClaim's
// existing delegation to billing.Service above.

// ListWalkInSales is a chemist's "Today's Sales" list, optionally filtered by status.
func (s *Service) ListWalkInSales(ctx context.Context, tenantID uuid.UUID, status string) ([]*ent.WalkInSale, error) {
	return s.billing.ListWalkInSales(ctx, tenantID, status)
}

// CollectWalkInSale collects payment for one pending walk-in sale.
func (s *Service) CollectWalkInSale(ctx context.Context, tenantID, saleID uuid.UUID, req billing.CollectWalkInSaleRequest) (*ent.WalkInSale, error) {
	return s.billing.CollectWalkInSale(ctx, tenantID, saleID, req)
}

// WaiveWalkInSale writes off a pending walk-in sale without collecting payment.
func (s *Service) WaiveWalkInSale(ctx context.Context, tenantID, saleID uuid.UUID) (*ent.WalkInSale, error) {
	return s.billing.WaiveWalkInSale(ctx, tenantID, saleID)
}

// ListControlledSubstanceLogs lists the dual-witness register, newest first.
func (s *Service) ListControlledSubstanceLogs(ctx context.Context, tenantID uuid.UUID) ([]*ent.ControlledSubstanceLog, error) {
	return s.client.ControlledSubstanceLog.Query().
		Where(controlledsubstancelog.TenantID(tenantID)).
		Order(ent.Desc(controlledsubstancelog.FieldDispensedAt)).
		Limit(200).
		All(ctx)
}
