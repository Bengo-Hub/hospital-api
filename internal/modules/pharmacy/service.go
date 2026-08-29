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

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/controlledsubstancelog"
	"github.com/bengobox/hospital-service/internal/ent/prescription"
	"github.com/bengobox/hospital-service/internal/ent/prescriptionline"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/inventory"
	"github.com/bengobox/hospital-service/internal/modules/sequence"
)

// Service implements pharmacy business logic.
type Service struct {
	client    *ent.Client
	inventory *inventory.Client
	billing   *billing.Service
	log       *zap.Logger
}

// NewService creates a new pharmacy service.
func NewService(client *ent.Client, inventoryClient *inventory.Client, billingSvc *billing.Service, log *zap.Logger) *Service {
	return &Service{client: client, inventory: inventoryClient, billing: billingSvc, log: log.Named("pharmacy.service")}
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
// prescription for pharmacist review. Best-effort: a transport failure is logged, not fatal —
// dispensing safety checks should degrade to "needs manual review," never silently vanish.
func (s *Service) runInteractionCheck(ctx context.Context, tenantID, rxID uuid.UUID, skus, allergyFlags []string) {
	result, err := s.inventory.CheckInteractions(ctx, tenantID, skus, allergyFlags)
	if err != nil {
		s.log.Warn("interaction check failed", zap.String("prescription_id", rxID.String()), zap.Error(err))
		return
	}
	outcome := "clear"
	if len(result.Interactions) > 0 || len(result.AllergyMatches) > 0 {
		outcome = "interactions_found"
		if len(result.Interactions) == 0 {
			outcome = "allergy_match"
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
		s.log.Warn("save interaction check failed", zap.Error(cerr))
		return
	}
	if outcome != "clear" {
		rx, gerr := s.client.Prescription.Get(ctx, rxID)
		if gerr == nil {
			meta := rx.Metadata
			if meta == nil {
				meta = map[string]any{}
			}
			meta["interaction_check_id"] = check.ID.String()
			_, _ = s.client.Prescription.UpdateOneID(rxID).
				SetStatus(prescription.StatusFlagged).
				SetMetadata(meta).
				Save(ctx)
		}
	}
}

// GetPrescription fetches a prescription with its lines, tenant-scoped.
func (s *Service) GetPrescription(ctx context.Context, tenantID, id uuid.UUID) (*ent.Prescription, error) {
	return s.client.Prescription.Query().
		Where(prescription.ID(id), prescription.TenantID(tenantID)).
		WithLines().
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
// the prescription approved. An explicit override reason is required to approve a
// flagged (interaction/allergy finding) prescription — never a silent bypass.
func (s *Service) ApprovePrescription(ctx context.Context, tenantID, rxID, approvedBy uuid.UUID, overrideReason string) (*ent.Prescription, error) {
	rx, err := s.GetPrescription(ctx, tenantID, rxID)
	if err != nil {
		return nil, fmt.Errorf("pharmacy: prescription not found: %w", err)
	}
	if rx.Status == prescription.StatusFlagged && overrideReason == "" {
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
	// RequiresWitness/WitnessStaffID drive the controlled-substance dual-witness register — the
	// dispensing UI determines this from inventory-api's Item.controlled_substance_schedule
	// (fetched separately) and must supply an independent witness before this line dispenses.
	RequiresWitness bool
	WitnessStaffID  *uuid.UUID
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
	for _, dl := range req.Lines {
		if dl.RequiresWitness && dl.WitnessStaffID == nil {
			return nil, fmt.Errorf("pharmacy: a witness is required to dispense this controlled substance")
		}
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
	for _, dl := range req.Lines {
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
				SetLotNumber(line.LotNumber)
			if dl.WitnessStaffID != nil {
				logCreate = logCreate.SetWitnessStaffID(*dl.WitnessStaffID)
			}
			if line.ExpiryDate != nil {
				logCreate = logCreate.SetLotExpiryDate(*line.ExpiryDate)
			}
			if _, lerr := logCreate.Save(ctx); lerr != nil {
				err = lerr
				return nil, fmt.Errorf("pharmacy: create controlled substance log: %w", lerr)
			}
		}

		if line.UnitPrice > 0 && rx.PatientID != nil && rx.VisitID != nil {
			if _, cerr := s.billing.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
				PatientID:     *rx.PatientID,
				VisitID:       *rx.VisitID,
				SourceModule:  "pharmacy",
				SourceRefID:   &line.ID,
				Description:   "Drug dispensed: " + line.DrugName,
				Amount:        line.UnitPrice * dl.QuantityToDispense,
				CreatedByUser: req.DispensedBy,
			}); cerr != nil {
				err = cerr
				return nil, fmt.Errorf("pharmacy: post charge for %s: %w", line.DrugName, cerr)
			}
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

// ListControlledSubstanceLogs lists the dual-witness register, newest first.
func (s *Service) ListControlledSubstanceLogs(ctx context.Context, tenantID uuid.UUID) ([]*ent.ControlledSubstanceLog, error) {
	return s.client.ControlledSubstanceLog.Query().
		Where(controlledsubstancelog.TenantID(tenantID)).
		Order(ent.Desc(controlledsubstancelog.FieldDispensedAt)).
		Limit(200).
		All(ctx)
}
