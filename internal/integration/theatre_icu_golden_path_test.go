package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent/bed"
	"github.com/bengobox/hospital-service/internal/ent/theatrebooking"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/icu"
	"github.com/bengobox/hospital-service/internal/modules/inpatient"
	"github.com/bengobox/hospital-service/internal/modules/patients"
	"github.com/bengobox/hospital-service/internal/modules/refdata"
	"github.com/bengobox/hospital-service/internal/modules/theatre"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

// TestTheatreGoldenPath walks Sprint 7's surgery-scheduling half: create a booking (asserts the
// prepayment gate mirrors lab's exact awaiting_payment behavior) -> reject an overlapping booking
// in the same room -> a non-overlapping booking in the SAME room succeeds -> pay -> activate ->
// start -> complete, asserting the fee posted onto the visit's admission account (the same
// admission-aware billing.PostCharge routing Sprint 6 introduced, now exercised by a second
// department).
func TestTheatreGoldenPath(t *testing.T) {
	client := newTestClient(t)
	log := zap.NewNop()
	ctx := context.Background()

	treSrv := fakeTreasuryServer(t)
	treClient := treasury.NewClient(treSrv.URL, "test-key", log)

	tenantID := uuid.New()
	outletID := uuid.New()

	if err := refdata.SeedFacilityBillableItems(ctx, client, tenantID, "hospital", log); err != nil {
		t.Fatalf("seed billable items: %v", err)
	}

	billingSvc := billing.NewService(client, treClient, log)
	patientsSvc := patients.NewService(client, billingSvc, log)
	inpatientSvc := inpatient.NewService(client, billingSvc, log)
	theatreSvc := theatre.NewService(client, billingSvc, log)

	patient, err := patientsSvc.RegisterPatient(ctx, tenantID, patients.RegisterPatientRequest{
		FullName: "Achieng Odhiambo", Sex: "F", Phone: "0733445566", OutletID: outletID,
	})
	if err != nil {
		t.Fatalf("register patient: %v", err)
	}
	visit, err := patientsSvc.CheckInVisit(ctx, tenantID, patients.CheckInVisitRequest{
		PatientID: patient.ID, OutletID: outletID, VisitType: "OPD", ChiefComplaint: "Appendicitis",
	})
	if err != nil {
		t.Fatalf("check in visit: %v", err)
	}

	// Admit so the theatre fee lands on the admission account, not a fresh OPD one — proves the
	// Sprint 6 admission-aware billing routing generalizes to a second department untouched.
	ward, err := inpatientSvc.CreateWard(ctx, tenantID, outletID, "Surgical Ward", 5)
	if err != nil {
		t.Fatalf("create ward: %v", err)
	}
	wardBed, err := inpatientSvc.CreateBed(ctx, tenantID, ward.ID, "SW-01")
	if err != nil {
		t.Fatalf("create bed: %v", err)
	}
	adm, err := inpatientSvc.Admit(ctx, tenantID, inpatient.AdmitRequest{VisitID: visit.ID, BedID: wardBed.ID, AdmittedBy: uuid.New()})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}

	scheduledAt := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	fee := 25000.0
	booking, err := theatreSvc.CreateBooking(ctx, tenantID, theatre.CreateBookingRequest{
		VisitID: visit.ID, TheatreRoom: "OT-1", SurgeryType: "Appendectomy",
		ScheduledAt: scheduledAt, DurationMinutes: 90, FeeAmount: &fee, CreatedBy: uuid.New(),
	})
	if err != nil {
		t.Fatalf("create booking: %v", err)
	}
	// "hospital" tier seeds THEATRE_FEE with requires_prepayment=true.
	if booking.Status != theatrebooking.StatusAwaitingPayment {
		t.Fatalf("booking status = %q, want %q (hospital tier requires theatre prepayment)", booking.Status, theatrebooking.StatusAwaitingPayment)
	}

	// Overlapping slot in the SAME room must be rejected.
	overlapStart := scheduledAt.Add(30 * time.Minute) // inside the first booking's 90-minute window
	if _, err := theatreSvc.CreateBooking(ctx, tenantID, theatre.CreateBookingRequest{
		VisitID: visit.ID, TheatreRoom: "OT-1", SurgeryType: "Hernia repair",
		ScheduledAt: overlapStart, DurationMinutes: 60, CreatedBy: uuid.New(),
	}); err == nil {
		t.Fatalf("expected an overlapping booking in the same room to be rejected")
	}

	// A booking starting exactly when the first ends does NOT overlap.
	backToBackStart := scheduledAt.Add(90 * time.Minute)
	if _, err := theatreSvc.CreateBooking(ctx, tenantID, theatre.CreateBookingRequest{
		VisitID: visit.ID, TheatreRoom: "OT-1", SurgeryType: "Wound dressing",
		ScheduledAt: backToBackStart, DurationMinutes: 30, CreatedBy: uuid.New(),
	}); err != nil {
		t.Fatalf("back-to-back booking in the same room should be allowed: %v", err)
	}

	// A DIFFERENT room at the exact same time must be allowed.
	if _, err := theatreSvc.CreateBooking(ctx, tenantID, theatre.CreateBookingRequest{
		VisitID: visit.ID, TheatreRoom: "OT-2", SurgeryType: "Cesarean section",
		ScheduledAt: scheduledAt, DurationMinutes: 60, CreatedBy: uuid.New(),
	}); err != nil {
		t.Fatalf("booking in a different room at the same time should be allowed: %v", err)
	}

	// Pay the procedure fee, then activate.
	_, admCharges, err := billingSvc.GetAccountByAdmission(ctx, tenantID, adm.ID)
	if err != nil {
		t.Fatalf("get admission account: %v", err)
	}
	theatreCharge := chargeBySourceModule(t, admCharges, "theatre")
	if theatreCharge.Amount != fee {
		t.Fatalf("theatre charge amount = %v, want %v (must land on the admission account)", theatreCharge.Amount, fee)
	}
	if _, err := billingSvc.CollectCharge(ctx, tenantID, theatreCharge.ID, billing.CollectChargeRequest{
		PaymentMethod: "cash", CollectedBy: uuid.New(),
	}); err != nil {
		t.Fatalf("collect theatre charge: %v", err)
	}
	booking, err = theatreSvc.ActivateIfPaid(ctx, tenantID, booking.ID)
	if err != nil {
		t.Fatalf("activate booking: %v", err)
	}
	if booking.Status != theatrebooking.StatusScheduled {
		t.Fatalf("booking status after payment = %q, want %q", booking.Status, theatrebooking.StatusScheduled)
	}

	// Biomedical Equipment linkage (2026-09-02, brought forward from Sprint 9) — a theatre
	// booking can reference inventory-api Asset IDs (e.g. an anaesthesia machine).
	anaesthesiaMachineID := uuid.New()
	bookingWithEquipment, err := theatreSvc.SetEquipment(ctx, tenantID, booking.ID, []uuid.UUID{anaesthesiaMachineID})
	if err != nil {
		t.Fatalf("set theatre equipment: %v", err)
	}
	if len(bookingWithEquipment.EquipmentAssetIds) != 1 || bookingWithEquipment.EquipmentAssetIds[0] != anaesthesiaMachineID {
		t.Fatalf("booking equipment_asset_ids = %v, want [%v]", bookingWithEquipment.EquipmentAssetIds, anaesthesiaMachineID)
	}

	// Checklist, start, complete.
	if _, err := theatreSvc.UpdateChecklist(ctx, tenantID, booking.ID, map[string]bool{"consent_signed": true, "site_marked": true}); err != nil {
		t.Fatalf("update checklist: %v", err)
	}
	booking, err = theatreSvc.StartSurgery(ctx, tenantID, booking.ID)
	if err != nil {
		t.Fatalf("start surgery: %v", err)
	}
	if booking.Status != theatrebooking.StatusInProgress || booking.StartedAt == nil {
		t.Fatalf("booking after start: status=%q startedAt=%v, want in_progress/non-nil", booking.Status, booking.StartedAt)
	}
	booking, err = theatreSvc.CompleteSurgery(ctx, tenantID, booking.ID)
	if err != nil {
		t.Fatalf("complete surgery: %v", err)
	}
	if booking.Status != theatrebooking.StatusCompleted || booking.CompletedAt == nil {
		t.Fatalf("booking after complete: status=%q completedAt=%v, want completed/non-nil", booking.Status, booking.CompletedAt)
	}

	// ── ICU episode lifecycle, tied to the same admission ──────────────────────────────────────
	icuSvc := icu.NewService(client, log)
	episode, err := icuSvc.StartEpisode(ctx, tenantID, icu.StartEpisodeRequest{
		AdmissionID: adm.ID, SeverityFlag: "critical", MonitoringNotes: "Post-op recovery, vitals hourly",
	})
	if err != nil {
		t.Fatalf("start ICU episode: %v", err)
	}
	if episode.BedID != wardBed.ID {
		t.Fatalf("ICU episode bed_id = %s, want the admission's current bed %s", episode.BedID, wardBed.ID)
	}

	// A second concurrent episode for the SAME admission must be rejected.
	if _, err := icuSvc.StartEpisode(ctx, tenantID, icu.StartEpisodeRequest{AdmissionID: adm.ID}); err == nil {
		t.Fatalf("expected a second concurrent ICU episode for the same admission to be rejected")
	}

	stable := "stable"
	updated, err := icuSvc.UpdateEpisode(ctx, tenantID, episode.ID, icu.UpdateEpisodeRequest{SeverityFlag: &stable})
	if err != nil {
		t.Fatalf("update ICU episode: %v", err)
	}
	if string(updated.SeverityFlag) != stable {
		t.Fatalf("severity_flag after update = %q, want %q", updated.SeverityFlag, stable)
	}

	// Biomedical Equipment linkage — an ICU episode can reference inventory-api Asset IDs (e.g. a
	// ventilator), updated via the same UpdateEpisode call as severity/notes.
	ventilatorID := uuid.New()
	withEquipment, err := icuSvc.UpdateEpisode(ctx, tenantID, episode.ID, icu.UpdateEpisodeRequest{
		EquipmentAssetIDs: &[]uuid.UUID{ventilatorID},
	})
	if err != nil {
		t.Fatalf("set ICU episode equipment: %v", err)
	}
	if len(withEquipment.EquipmentAssetIds) != 1 || withEquipment.EquipmentAssetIds[0] != ventilatorID {
		t.Fatalf("episode equipment_asset_ids = %v, want [%v]", withEquipment.EquipmentAssetIds, ventilatorID)
	}

	ended, err := icuSvc.EndEpisode(ctx, tenantID, episode.ID)
	if err != nil {
		t.Fatalf("end ICU episode: %v", err)
	}
	if ended.EndedAt == nil {
		t.Fatalf("episode ended_at is nil after EndEpisode")
	}

	active, err := icuSvc.ListEpisodes(ctx, tenantID, true)
	if err != nil {
		t.Fatalf("list active episodes: %v", err)
	}
	for _, e := range active {
		if e.ID == episode.ID {
			t.Fatalf("ended episode still appears in the active-only list")
		}
	}

	// Sanity: the bed the ICU episode snapshotted is still the admission's real current bed
	// (no transfer happened in this test).
	freshBed, err := client.Bed.Query().Where(bed.ID(wardBed.ID)).Only(ctx)
	if err != nil {
		t.Fatalf("get bed: %v", err)
	}
	if freshBed.Status != bed.StatusOccupied {
		t.Fatalf("bed status = %q, want %q (admission still active)", freshBed.Status, bed.StatusOccupied)
	}
}
