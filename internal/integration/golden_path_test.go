// Package integration exercises hospital-api's real service-layer code (patients, consultation,
// lab, pharmacy, billing) end to end against an in-memory ent client and fake inventory-api/
// treasury-api servers — the automated form of the "register -> triage -> examine -> lab ->
// prescribe -> dispense -> checkout/insurance" acceptance-gate walkthrough this service's own
// docs (migration-pos-pharmacy.md, docs/sprints/*.md) describe. Uses modernc.org/sqlite (pure Go)
// rather than mattn/go-sqlite3, since the latter needs cgo and this environment has no C
// toolchain configured — see newTestClient's doc comment for the exact wiring.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/billablecharge"
	"github.com/bengobox/hospital-service/internal/ent/examinationrecord"
	"github.com/bengobox/hospital-service/internal/ent/laborder"
	"github.com/bengobox/hospital-service/internal/ent/patientaccount"
	"github.com/bengobox/hospital-service/internal/ent/patientvisit"
	"github.com/bengobox/hospital-service/internal/ent/prescription"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/consultation"
	"github.com/bengobox/hospital-service/internal/modules/inventory"
	"github.com/bengobox/hospital-service/internal/modules/lab"
	"github.com/bengobox/hospital-service/internal/modules/patients"
	"github.com/bengobox/hospital-service/internal/modules/pharmacy"
	"github.com/bengobox/hospital-service/internal/modules/refdata"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

// newTestClient builds an in-memory, schema-migrated ent client. Bypasses the generated
// enttest.Open helper deliberately: enttest.Open's underlying ent.Open(driverName, ...) only
// recognizes the literal driver names "sqlite3"/"mysql"/"postgres" (see internal/ent/client.go's
// Open), which is the name mattn/go-sqlite3 (cgo) registers under — not modernc.org/sqlite's
// "sqlite". entsql.OpenDB lets the dialect (SQLite) and the driver be specified independently of
// that name-matching, which is the documented pattern for a pure-Go sqlite driver with ent.
func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	// Deliberately NOT capping MaxOpenConns at 1: several service methods (e.g.
	// lab.Service.CreateOrder) open their own transaction and then, within it, still issue a
	// separate non-transactional read via the shared *ent.Client (fine against a real Postgres
	// pool with multiple connections) — capping this test DB to one connection deadlocks that
	// exact pattern, since the open transaction holds the only connection while the non-tx read
	// waits forever for a free one. cache=shared keeps the in-memory database alive across
	// multiple connections, which is what actually prevents it from vanishing.
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// fakeInventoryServer stands in for inventory-api's S2S surface (see internal/modules/inventory
// .Client): a clear drug-interaction result, a reservation on create, and one consumed lot for a
// non-controlled drug on consume — enough to exercise pharmacy.Service's real approve/dispense
// flow without a live inventory-api.
func fakeInventoryServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/check-interactions"):
			writeJSON(w, map[string]any{"interactions": []any{}, "allergy_matches": []any{}})
		case strings.HasSuffix(r.URL.Path, "/consume"):
			writeJSON(w, map[string]any{
				"status": "consumed",
				"lots_consumed": []map[string]any{{
					"sku": "PARA500", "lot_id": uuid.New().String(), "lot_number": "LOT-0001",
					"expiry_date": time.Now().AddDate(1, 0, 0).Format(time.RFC3339), "quantity": 15,
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/release"):
			writeJSON(w, map[string]any{})
		case strings.HasSuffix(r.URL.Path, "/reservations"):
			writeJSON(w, map[string]any{
				"id": uuid.New().String(), "status": "reserved", "items": []any{},
				"created_at": time.Now().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeTreasuryServer stands in for treasury-api's S2S surface (see internal/modules/treasury
// .Client): invoice/payment-intent creation for the cash-collect path, and an immediately-
// "approved" insurance claim for the insurance-settlement path — enough to exercise both of
// billing.Service's two settlement routes without a live treasury-api.
func fakeTreasuryServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/invoices"):
			writeJSON(w, map[string]any{
				"id": uuid.New().String(), "invoice_no": "INV-TEST-0001",
				"total_amount": 0, "amount_paid": 0, "status": "pending",
			})
		case strings.HasSuffix(r.URL.Path, "/payments/intents"):
			writeJSON(w, map[string]any{"id": uuid.New().String(), "status": "succeeded"})
		case strings.HasSuffix(r.URL.Path, "/insurance/claims"):
			writeJSON(w, map[string]any{
				"id": uuid.New().String(), "status": "approved", "claim_reference": "MED-TEST-0001",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chargeBySourceModule(t *testing.T, charges []*ent.BillableCharge, module string) *ent.BillableCharge {
	t.Helper()
	for _, c := range charges {
		if c.SourceModule == module {
			return c
		}
	}
	t.Fatalf("no %s charge found among %d charges", module, len(charges))
	return nil
}

// TestGoldenPath walks the full clinical+billing chain this service's roadmap describes as its
// Phase 8 acceptance gate, against real service-layer code. Deliberately does not exercise the
// controlled-substance witness flow (pharmacy.Service.VerifyWitness) — that calls a real
// *authclient.Validator (JWKS-based), which would need its own fake JWKS/RSA-keypair setup, and
// was already reviewed commit-by-commit in this migration's parity-audit chain (see
// .claude/plans/pharmacy-to-hospital-service-migration-2026-08-29.md). Uses a non-controlled drug
// so every dispense line has RequiresWitness=false.
func TestGoldenPath(t *testing.T) {
	client := newTestClient(t)
	log := zap.NewNop()
	ctx := context.Background()

	invSrv := fakeInventoryServer(t)
	treSrv := fakeTreasuryServer(t)
	invClient := inventory.NewClient(invSrv.URL, "test-key", log)
	treClient := treasury.NewClient(treSrv.URL, "test-key", log)

	tenantID := uuid.New()
	outletID := uuid.New()

	if err := refdata.SeedGlobalDiagnosisCatalog(ctx, client, log); err != nil {
		t.Fatalf("seed diagnosis catalog: %v", err)
	}
	if err := refdata.SeedGlobalLabTestCatalog(ctx, client, log); err != nil {
		t.Fatalf("seed lab test catalog: %v", err)
	}
	// "facility" tier: lab requires prepayment, records/consultation/pharmacy don't — exercises
	// the awaiting_payment gate, not just the un-gated happy path.
	if err := refdata.SeedFacilityBillableItems(ctx, client, tenantID, "facility", log); err != nil {
		t.Fatalf("seed billable items: %v", err)
	}

	billingSvc := billing.NewService(client, treClient, log)
	patientsSvc := patients.NewService(client, billingSvc, log)
	consultationSvc := consultation.NewService(client, billingSvc, log)
	labSvc := lab.NewService(client, billingSvc, log)
	pharmacySvc := pharmacy.NewService(client, invClient, billingSvc, log, nil, nil, nil, []byte("test-witness-secret"))

	// 1. Register + check in — asserts the registration fee posts (landed the same day this test
	// was written; verified here end to end for the first time in an automated test).
	patient, err := patientsSvc.RegisterPatient(ctx, tenantID, patients.RegisterPatientRequest{
		FullName: "Jane Wanjiru", Sex: "F", Phone: "0712345678", OutletID: outletID,
	})
	if err != nil {
		t.Fatalf("register patient: %v", err)
	}
	visit, err := patientsSvc.CheckInVisit(ctx, tenantID, patients.CheckInVisitRequest{
		PatientID: patient.ID, OutletID: outletID, VisitType: "OPD", ChiefComplaint: "Fever and cough",
	})
	if err != nil {
		t.Fatalf("check in visit: %v", err)
	}
	if visit.Status != patientvisit.StatusRegistered {
		t.Fatalf("visit status = %q, want %q", visit.Status, patientvisit.StatusRegistered)
	}

	acct, charges, err := billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get account after check-in: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("expected 1 charge (registration fee) after check-in, got %d", len(charges))
	}
	if acct.Balance != 150 { // REGISTRATION_FEE, "facility" tier, first_visit
		t.Fatalf("balance after registration = %v, want 150", acct.Balance)
	}

	// 2. Triage.
	if _, err := patientsSvc.RecordTriage(ctx, tenantID, patients.RecordTriageRequest{
		VisitID: visit.ID, TakenBy: uuid.New(), Priority: "urgent", Notes: "BP 130/85, temp 38.2",
	}); err != nil {
		t.Fatalf("record triage: %v", err)
	}

	// 3. Examination + diagnosis — asserts the consultation fee posts too.
	exam, err := consultationSvc.RecordExamination(ctx, tenantID, consultation.RecordExaminationRequest{
		VisitID: visit.ID, ClinicianID: uuid.New(), QueueType: "doctor",
		ChiefComplaint: "Fever and cough", DiagnosisCode: "CA40", DiagnosisName: "Pneumonia, organism unspecified",
	})
	if err != nil {
		t.Fatalf("record examination: %v", err)
	}

	acct, charges, err = billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get account after examination: %v", err)
	}
	if len(charges) != 2 {
		t.Fatalf("expected 2 charges (records+consultation) after examination, got %d", len(charges))
	}
	if acct.Balance != 650 { // 150 registration + 500 consultation ("facility" tier)
		t.Fatalf("balance after examination = %v, want 650", acct.Balance)
	}

	// 4. Refer to lab.
	if _, err := consultationSvc.CreateReferral(ctx, tenantID, consultation.CreateReferralRequest{
		VisitID: visit.ID, ReferredTo: "lab", Reason: "Rule out bacterial pneumonia", ReferredBy: exam.ClinicianID,
	}); err != nil {
		t.Fatalf("create referral: %v", err)
	}
	visit, err = patientsSvc.GetVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get visit after referral: %v", err)
	}
	if visit.Status != patientvisit.StatusAwaitingLab {
		t.Fatalf("visit status after lab referral = %q, want %q", visit.Status, patientvisit.StatusAwaitingLab)
	}

	// 5. Lab order — "facility" tier requires lab prepayment, so this must land awaiting_payment.
	examID := exam.ID
	order, err := labSvc.CreateOrder(ctx, tenantID, lab.CreateOrderRequest{
		VisitID: visit.ID, ExaminationID: &examID, OrderedBy: exam.ClinicianID, TestCodes: []string{"FBC"},
	})
	if err != nil {
		t.Fatalf("create lab order: %v", err)
	}
	if order.Status != laborder.StatusAwaitingPayment {
		t.Fatalf("lab order status = %q, want %q (facility tier requires lab prepayment)", order.Status, laborder.StatusAwaitingPayment)
	}
	_, orderLines, err := labSvc.GetOrder(ctx, tenantID, order.ID)
	if err != nil {
		t.Fatalf("get lab order lines: %v", err)
	}
	if len(orderLines) != 1 {
		t.Fatalf("expected 1 lab order line, got %d", len(orderLines))
	}

	acct, charges, err = billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get account after lab order: %v", err)
	}
	labCharge := chargeBySourceModule(t, charges, "lab")
	if _, err := billingSvc.CollectCharge(ctx, tenantID, labCharge.ID, billing.CollectChargeRequest{
		PaymentMethod: "cash", CollectedBy: uuid.New(),
	}); err != nil {
		t.Fatalf("collect lab charge: %v", err)
	}
	order, err = labSvc.ActivateIfPaid(ctx, tenantID, order.ID)
	if err != nil {
		t.Fatalf("activate lab order after payment: %v", err)
	}
	if order.Status != laborder.StatusRequested {
		t.Fatalf("lab order status after payment = %q, want %q", order.Status, laborder.StatusRequested)
	}

	// 6. Result entry — asserts the visit advances to lab_complete and the examination reopens
	// (the exact transition this migration's Wave 3 parity-audit fix added).
	if _, err := labSvc.EnterResult(ctx, tenantID, orderLines[0].ID, lab.EnterResultRequest{
		ResultValue: "12.4", Unit: "g/dL", Flag: "normal", ResultedBy: uuid.New(),
	}); err != nil {
		t.Fatalf("enter lab result: %v", err)
	}
	visit, err = patientsSvc.GetVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get visit after lab result: %v", err)
	}
	if visit.Status != patientvisit.StatusLabComplete {
		t.Fatalf("visit status after lab result = %q, want %q", visit.Status, patientvisit.StatusLabComplete)
	}
	examAfterLab, err := consultationSvc.GetExamination(ctx, tenantID, exam.ID)
	if err != nil {
		t.Fatalf("get examination after lab result: %v", err)
	}
	if examAfterLab.Status != examinationrecord.StatusInProgress {
		t.Fatalf("examination status after lab result = %q, want %q", examAfterLab.Status, examinationrecord.StatusInProgress)
	}

	// 7. Prescribe (non-controlled drug, no witness needed) -> approve -> dispense.
	patientID := patient.ID
	visitID := visit.ID
	rx, err := pharmacySvc.CreatePrescription(ctx, tenantID, pharmacy.CreatePrescriptionRequest{
		OutletID: outletID, PatientID: &patientID, VisitID: &visitID, ExaminationID: &examID,
		PrescriberName: "Dr. Achieng", PatientName: patient.FullName,
		Lines: []pharmacy.PrescriptionLineInput{{
			InventoryItemSKU: "PARA500", DrugName: "Paracetamol 500mg", Dosage: "1 tab", Form: "tablet",
			Instructions: "TDS x5 days", QuantityPrescribed: 15, UnitPrice: 20,
		}},
	})
	if err != nil {
		t.Fatalf("create prescription: %v", err)
	}
	if len(rx.Edges.Lines) != 1 {
		t.Fatalf("expected 1 prescription line, got %d", len(rx.Edges.Lines))
	}
	lineID := rx.Edges.Lines[0].ID

	approved, err := pharmacySvc.ApprovePrescription(ctx, tenantID, rx.ID, uuid.New(), "")
	if err != nil {
		t.Fatalf("approve prescription: %v", err)
	}
	if approved.Status != prescription.StatusApproved {
		t.Fatalf("prescription status after approve = %q, want %q", approved.Status, prescription.StatusApproved)
	}

	dispensed, err := pharmacySvc.Dispense(ctx, tenantID, rx.ID, pharmacy.DispenseRequest{
		DispensedBy: uuid.New(), OutletID: outletID, PatientName: patient.FullName,
		Lines: []pharmacy.DispenseLineInput{{LineID: lineID, QuantityToDispense: 15}},
	})
	if err != nil {
		t.Fatalf("dispense: %v", err)
	}
	if dispensed.Status != prescription.StatusDispensed {
		t.Fatalf("prescription status after dispense = %q, want %q", dispensed.Status, prescription.StatusDispensed)
	}

	// 8. Settle the dispense charge via insurance — asserts EXEMPTED (not PAID), the status this
	// service's own KenyaEMR-audit doc pass recommended and this migration's Sprint 5 remainder
	// added.
	acct, charges, err = billingSvc.GetAccountByVisit(ctx, tenantID, visit.ID)
	if err != nil {
		t.Fatalf("get account after dispense: %v", err)
	}
	pharmacyCharge := chargeBySourceModule(t, charges, "pharmacy")
	claimResult, err := billingSvc.SubmitInsuranceClaim(ctx, tenantID, acct.ID, billing.SubmitInsuranceClaimRequest{
		ProviderID: uuid.New(), ChargeIDs: []uuid.UUID{pharmacyCharge.ID},
	})
	if err != nil {
		t.Fatalf("submit insurance claim: %v", err)
	}
	if !claimResult.Accepted {
		t.Fatalf("expected the fake treasury claim to be accepted")
	}
	if claimResult.Charges[0].Status != billablecharge.StatusExempted {
		t.Fatalf("pharmacy charge status after accepted claim = %q, want %q", claimResult.Charges[0].Status, billablecharge.StatusExempted)
	}

	// 9. Settle everything else (registration + consultation were never paid in this test) so the
	// account reaches a genuine zero balance — the discharge/checkout gate every facility tier
	// above chemist relies on.
	if _, err := billingSvc.SettleAccount(ctx, tenantID, acct.ID, billing.CollectChargeRequest{
		PaymentMethod: "cash", CollectedBy: uuid.New(),
	}, nil); err != nil {
		t.Fatalf("settle account: %v", err)
	}
	finalAcct, _, err := billingSvc.GetAccount(ctx, tenantID, acct.ID)
	if err != nil {
		t.Fatalf("get final account: %v", err)
	}
	if finalAcct.Balance != 0 {
		t.Fatalf("final balance = %v, want 0", finalAcct.Balance)
	}
	if finalAcct.Status != patientaccount.StatusSettled {
		t.Fatalf("final account status = %q, want %q", finalAcct.Status, patientaccount.StatusSettled)
	}
}
