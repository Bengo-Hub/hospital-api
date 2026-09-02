package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent/admission"
	"github.com/bengobox/hospital-service/internal/ent/bed"
	"github.com/bengobox/hospital-service/internal/ent/patientaccount"
	"github.com/bengobox/hospital-service/internal/ent/patienttransfer"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/inpatient"
	"github.com/bengobox/hospital-service/internal/modules/patients"
	"github.com/bengobox/hospital-service/internal/modules/refdata"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

// TestInpatientGoldenPath walks Sprint 6's admission-to-discharge lifecycle against real
// service-layer code: register/check-in (OPD account) -> admit (opens a SEPARATE admission
// account, per docs/architecture.md "Distributed Billing & Patient Accounts") -> a department
// posts a charge while admitted (asserts it routes onto the ADMISSION account, not the OPD one —
// the single riskiest new behavior this sprint added to billing.Service.PostCharge) -> an
// intra-facility transfer (asserts the bed swap + PatientTransfer row) -> discharge blocked on an
// outstanding balance -> settle -> discharge succeeds (ward day-rate charge posted, bed freed to
// cleaning, visit completed).
func TestInpatientGoldenPath(t *testing.T) {
	client := newTestClient(t)
	log := zap.NewNop()
	ctx := context.Background()

	treSrv := fakeTreasuryServer(t)
	treClient := treasury.NewClient(treSrv.URL, "test-key", log)

	tenantID := uuid.New()
	outletID := uuid.New()

	// "facility" tier seeds WARD_DAY_RATE=1500 (department=inpatient) alongside the registration/
	// consultation fees the OPD check-in below posts.
	if err := refdata.SeedFacilityBillableItems(ctx, client, tenantID, "facility", log); err != nil {
		t.Fatalf("seed billable items: %v", err)
	}

	billingSvc := billing.NewService(client, treClient, log)
	patientsSvc := patients.NewService(client, billingSvc, log)
	inpatientSvc := inpatient.NewService(client, billingSvc, log)

	// 1. Register + check in (OPD) — posts a registration-fee charge onto the visit's OWN account.
	patient, err := patientsSvc.RegisterPatient(ctx, tenantID, patients.RegisterPatientRequest{
		FullName: "Otieno Kamau", Sex: "M", Phone: "0722334455", OutletID: outletID,
	})
	if err != nil {
		t.Fatalf("register patient: %v", err)
	}
	visit, err := patientsSvc.CheckInVisit(ctx, tenantID, patients.CheckInVisitRequest{
		PatientID: patient.ID, OutletID: outletID, VisitType: "OPD", ChiefComplaint: "Severe abdominal pain",
	})
	if err != nil {
		t.Fatalf("check in visit: %v", err)
	}
	opdAcct, _, err := billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get OPD account: %v", err)
	}
	if opdAcct.Balance != 150 { // REGISTRATION_FEE, "facility" tier, first_visit
		t.Fatalf("OPD balance after check-in = %v, want 150", opdAcct.Balance)
	}

	// 2. Set up a ward with two beds.
	ward, err := inpatientSvc.CreateWard(ctx, tenantID, outletID, "General Ward", 10)
	if err != nil {
		t.Fatalf("create ward: %v", err)
	}
	bedA, err := inpatientSvc.CreateBed(ctx, tenantID, ward.ID, "GW-01")
	if err != nil {
		t.Fatalf("create bed A: %v", err)
	}
	bedB, err := inpatientSvc.CreateBed(ctx, tenantID, ward.ID, "GW-02")
	if err != nil {
		t.Fatalf("create bed B: %v", err)
	}

	// 3. Admit — asserts the visit flips to IPD/admitted, the bed occupies, and a NEW (separate)
	// admission account opens with a zero balance (nothing charged yet).
	adm, err := inpatientSvc.Admit(ctx, tenantID, inpatient.AdmitRequest{
		VisitID: visit.ID, BedID: bedA.ID, AdmittedBy: uuid.New(),
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if adm.Status != admission.StatusActive {
		t.Fatalf("admission status = %q, want %q", adm.Status, admission.StatusActive)
	}
	visit, err = patientsSvc.GetVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get visit after admit: %v", err)
	}
	if visit.Status != patientvisit.StatusAdmitted || visit.VisitType != patientvisit.VisitTypeIPD {
		t.Fatalf("visit after admit = status %q type %q, want admitted/IPD", visit.Status, visit.VisitType)
	}
	occupiedBedA, err := client.Bed.Get(ctx, bedA.ID)
	if err != nil {
		t.Fatalf("get bed A: %v", err)
	}
	if occupiedBedA.Status != bed.StatusOccupied {
		t.Fatalf("bed A status after admit = %q, want %q", occupiedBedA.Status, bed.StatusOccupied)
	}
	admAcct, _, err := billingSvc.GetAccountByAdmission(ctx, tenantID, adm.ID)
	if err != nil {
		t.Fatalf("get admission account: %v", err)
	}
	if admAcct.ID == opdAcct.ID {
		t.Fatalf("admission account must be separate from the OPD visit account")
	}
	if admAcct.Balance != 0 {
		t.Fatalf("admission balance right after admit = %v, want 0", admAcct.Balance)
	}
	if admAcct.SettlementRequiredBefore != patientaccount.SettlementRequiredBeforeDischarge {
		t.Fatalf("admission account settlement_required_before = %q, want %q", admAcct.SettlementRequiredBefore, patientaccount.SettlementRequiredBeforeDischarge)
	}

	// 4. A department (e.g. lab) posts a charge for this SAME visit while admitted — must route
	// onto the ADMISSION's account, not open a second OPD-style account for the same visit_id.
	tx, err := client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := billingSvc.PostCharge(ctx, tx, tenantID, billing.PostChargeRequest{
		PatientID: patient.ID, VisitID: visit.ID, SourceModule: "lab",
		Description: "FBC while admitted", Amount: 800, CreatedByUser: uuid.New(),
	}); err != nil {
		t.Fatalf("post charge while admitted: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit charge: %v", err)
	}
	admAcct, admCharges, err := billingSvc.GetAccountByAdmission(ctx, tenantID, adm.ID)
	if err != nil {
		t.Fatalf("get admission account after lab charge: %v", err)
	}
	if admAcct.Balance != 800 {
		t.Fatalf("admission balance after lab charge = %v, want 800 (must not touch the OPD account)", admAcct.Balance)
	}
	if len(admCharges) != 1 {
		t.Fatalf("expected 1 charge on the admission account, got %d", len(admCharges))
	}
	opdAcctAfter, _, err := billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get OPD account after lab charge: %v", err)
	}
	if opdAcctAfter.Balance != 150 {
		t.Fatalf("OPD account balance changed after an in-admission charge (%v), want unchanged 150", opdAcctAfter.Balance)
	}

	// 5. Intra-facility transfer to bed B — old bed frees to cleaning, new bed occupies, a
	// PatientTransfer row records the move.
	adm, transferAcct, err := inpatientSvc.Transfer(ctx, tenantID, adm.ID, inpatient.TransferRequest{
		TransferType: "intra_facility", ToBedID: &bedB.ID, Reason: "Closer to nursing station",
		TransferredBy: uuid.New(),
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if transferAcct == nil || transferAcct.ID != admAcct.ID {
		t.Fatalf("transfer must return the SAME admission account, unaffected by the move itself")
	}
	if adm.BedID != bedB.ID || adm.WardID != ward.ID {
		t.Fatalf("admission after transfer: bed=%s ward=%s, want bed=%s ward=%s", adm.BedID, adm.WardID, bedB.ID, ward.ID)
	}
	freedBedA, err := client.Bed.Get(ctx, bedA.ID)
	if err != nil {
		t.Fatalf("get bed A after transfer: %v", err)
	}
	if freedBedA.Status != bed.StatusCleaning {
		t.Fatalf("bed A status after transfer = %q, want %q", freedBedA.Status, bed.StatusCleaning)
	}
	occupiedBedB, err := client.Bed.Get(ctx, bedB.ID)
	if err != nil {
		t.Fatalf("get bed B after transfer: %v", err)
	}
	if occupiedBedB.Status != bed.StatusOccupied {
		t.Fatalf("bed B status after transfer = %q, want %q", occupiedBedB.Status, bed.StatusOccupied)
	}
	transfers, err := client.PatientTransfer.Query().Where(patienttransfer.AdmissionID(adm.ID)).All(ctx)
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 PatientTransfer row, got %d", len(transfers))
	}
	if transfers[0].FromBedID != bedA.ID || transfers[0].ToBedID == nil || *transfers[0].ToBedID != bedB.ID {
		t.Fatalf("transfer row from/to mismatch: from=%s to=%v", transfers[0].FromBedID, transfers[0].ToBedID)
	}

	// 6. Discharge attempt while the lab charge is still unpaid (registration fee on the OPD
	// account is irrelevant here — discharge only gates on the ADMISSION account).
	_, _, err = inpatientSvc.Discharge(ctx, tenantID, adm.ID, inpatient.DischargeRequest{
		DischargedBy: uuid.New(), Summary: "Recovered, stable",
	})
	if err == nil {
		t.Fatalf("expected discharge to be blocked by the outstanding lab charge")
	}
	outstanding, ok := err.(*inpatient.ErrOutstandingBalance)
	if !ok {
		t.Fatalf("expected *inpatient.ErrOutstandingBalance, got %T: %v", err, err)
	}
	if outstanding.Account.Balance <= 0 {
		t.Fatalf("ErrOutstandingBalance.Account.Balance = %v, want > 0", outstanding.Account.Balance)
	}

	// 7. Settle the admission account (this also posts the ward day-rate charge, since Discharge's
	// first attempt above already ran postWardCharges before hitting the balance gate).
	if _, err := billingSvc.SettleAccount(ctx, tenantID, admAcct.ID, billing.CollectChargeRequest{
		PaymentMethod: "cash", CollectedBy: uuid.New(),
	}, nil); err != nil {
		t.Fatalf("settle admission account: %v", err)
	}
	settledAcct, settledCharges, err := billingSvc.GetAccountByAdmission(ctx, tenantID, adm.ID)
	if err != nil {
		t.Fatalf("get admission account after settle: %v", err)
	}
	if settledAcct.Balance != 0 {
		t.Fatalf("admission balance after settling everything = %v, want 0", settledAcct.Balance)
	}
	wardCharge := chargeBySourceModule(t, settledCharges, "inpatient")
	if wardCharge.Amount != 1500 { // 1 night * WARD_DAY_RATE(1500) at "facility" tier
		t.Fatalf("ward charge amount = %v, want 1500 (1 night)", wardCharge.Amount)
	}

	// 8. Discharge succeeds now: bed frees to cleaning, visit completes.
	dischargedAdm, finalAcct, err := inpatientSvc.Discharge(ctx, tenantID, adm.ID, inpatient.DischargeRequest{
		DischargedBy: uuid.New(), Summary: "Recovered, stable",
	})
	if err != nil {
		t.Fatalf("discharge after settlement: %v", err)
	}
	if dischargedAdm.Status != admission.StatusDischarged || dischargedAdm.DischargedAt == nil {
		t.Fatalf("admission after discharge: status=%q dischargedAt=%v, want discharged/non-nil", dischargedAdm.Status, dischargedAdm.DischargedAt)
	}
	if finalAcct == nil || finalAcct.Balance != 0 {
		t.Fatalf("final admission account balance = %v, want 0", finalAcct)
	}
	finalBedB, err := client.Bed.Get(ctx, bedB.ID)
	if err != nil {
		t.Fatalf("get bed B after discharge: %v", err)
	}
	if finalBedB.Status != bed.StatusCleaning {
		t.Fatalf("bed B status after discharge = %q, want %q", finalBedB.Status, bed.StatusCleaning)
	}
	finalVisit, err := patientsSvc.GetVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get visit after discharge: %v", err)
	}
	if finalVisit.Status != patientvisit.StatusCompleted || finalVisit.DischargedAt == nil {
		t.Fatalf("visit after discharge: status=%q dischargedAt=%v, want completed/non-nil", finalVisit.Status, finalVisit.DischargedAt)
	}

	// The OPD account (registration fee) is untouched by any of the admission-side settlement —
	// distributed billing keeps the two ledgers genuinely separate.
	finalOPDAcct, _, err := billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get OPD account after discharge: %v", err)
	}
	if finalOPDAcct.Balance != 150 {
		t.Fatalf("OPD account balance after discharge = %v, want unchanged 150", finalOPDAcct.Balance)
	}
}
