// Command seed-demo-workflow drives 3 realistic patients through real hospital-api service-layer
// code (the same functions the HTTP handlers call, not raw SQL) so a demo/sandbox tenant has
// actual clinical workflow data to click through instead of empty worklists — registration,
// triage, consultation+diagnosis, lab orders (incl. one CRITICAL result, exercising the Sprint 3
// critical-result event), pharmacy (incl. one full dispense, exercising the same-session
// dispensed-visit-status fix), and one active inpatient admission with vitals/ward-round/MAR data.
//
// Deliberately never touches inventory-api or treasury-api: every prescription line omits
// InventoryItemSKU (pharmacy.Service only calls inventory when a line has one, see
// CreatePrescription/ApprovePrescription/Dispense's `len(skus) > 0`/`len(items) > 0` guards), and
// no charge is ever collected/settled (billing.Service.PostCharge — called by registration/
// consultation/lab/admission fees — never calls treasury; only CollectCharge/SettleAccount/
// SubmitInsuranceClaim do, none of which this script calls). Both clients are constructed
// disabled (empty baseURL/apiKey) so even an accidental call fails closed rather than reaching a
// real external service. Every charge this script posts is left OUTSTANDING — a realistic,
// clickable Billing Queue is more useful as a demo than one this script pre-settled.
//
// Usage:
//
//	POSTGRES_URL=... go run ./cmd/seed-demo-workflow -tenant-slug=codevertex-demo -outlet-code=AFYA
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bengobox/hospital-service/internal/ent"
	"github.com/bengobox/hospital-service/internal/ent/bed"
	"github.com/bengobox/hospital-service/internal/ent/outlet"
	"github.com/bengobox/hospital-service/internal/ent/patient"
	"github.com/bengobox/hospital-service/internal/ent/prescription"
	"github.com/bengobox/hospital-service/internal/ent/tenant"
	"github.com/bengobox/hospital-service/internal/ent/ward"
	"github.com/bengobox/hospital-service/internal/modules/billing"
	"github.com/bengobox/hospital-service/internal/modules/consultation"
	"github.com/bengobox/hospital-service/internal/modules/inpatient"
	"github.com/bengobox/hospital-service/internal/modules/inventory"
	"github.com/bengobox/hospital-service/internal/modules/lab"
	"github.com/bengobox/hospital-service/internal/modules/mar"
	"github.com/bengobox/hospital-service/internal/modules/patients"
	"github.com/bengobox/hospital-service/internal/modules/pharmacy"
	"github.com/bengobox/hospital-service/internal/modules/treasury"
)

func ip(v int) *int         { return &v }
func fp(v float64) *float64 { return &v }

func main() {
	slug := flag.String("tenant-slug", "", "tenant slug (required)")
	outletCode := flag.String("outlet-code", "", "outlet code (required)")
	flag.Parse()
	if *slug == "" || *outletCode == "" {
		log.Fatal("seed-demo-workflow: -tenant-slug and -outlet-code are required")
	}

	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/hospital?sslmode=disable"
	}
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()
	drv := entsql.OpenDB(dialect.Postgres, sqlDB)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	zlog, _ := zap.NewProduction()
	defer zlog.Sync()
	ctx := context.Background()

	t, err := client.Tenant.Query().Where(tenant.Slug(*slug)).Only(ctx)
	if err != nil {
		log.Fatalf("tenant %q not found: %v", *slug, err)
	}
	out, err := client.Outlet.Query().Where(outlet.TenantID(t.ID), outlet.Code(*outletCode)).Only(ctx)
	if err != nil {
		log.Fatalf("outlet %q not found for tenant %q: %v", *outletCode, *slug, err)
	}
	log.Printf("seeding demo workflow: tenant=%s outlet=%s (%s)", t.Slug, out.Code, out.Name)

	genWard, err := client.Ward.Query().Where(ward.TenantID(t.ID), ward.OutletID(out.ID), ward.Name("General Ward")).Only(ctx)
	if err != nil {
		log.Fatalf("General Ward not found (run cmd/seed-tenant first): %v", err)
	}
	availableBed, err := client.Bed.Query().Where(bed.TenantID(t.ID), bed.WardID(genWard.ID), bed.StatusEQ("available")).First(ctx)
	if err != nil {
		log.Fatalf("no available bed in General Ward: %v", err)
	}

	// Disabled S2S clients — never actually called (see file doc comment), constructed disabled so
	// even an accidental call fails closed instead of reaching a real external service.
	treClient := treasury.NewClient("", "", zlog)
	invClient := inventory.NewClient("", "", zlog)

	billingSvc := billing.NewService(client, treClient, zlog)
	patientsSvc := patients.NewService(client, billingSvc, zlog)
	consultationSvc := consultation.NewService(client, billingSvc, zlog)
	labSvc := lab.NewService(client, billingSvc, zlog)
	pharmacySvc := pharmacy.NewService(client, invClient, billingSvc, zlog, nil, nil, nil, []byte("demo-seed-witness-secret"))
	inpatientSvc := inpatient.NewService(client, billingSvc, zlog)
	marSvc := mar.NewService(client, zlog)

	doctorID := uuid.New()
	nurseID := uuid.New()
	labTechID := uuid.New()
	pharmacistID := uuid.New()
	log.Printf("demo actor ids (not real HospitalUser rows — for narrative consistency only): doctor=%s nurse=%s lab_tech=%s pharmacist=%s", doctorID, nurseID, labTechID, pharmacistID)

	// Idempotent by "does this patient have at least one Prescription row" — every one of this
	// script's 3 patients gets exactly one Prescription near the end of their sequence, so its
	// presence is a reasonable proxy for "this patient's full sequence completed", unlike checking
	// Patient existence alone (a prior run of this exact script was killed by an external timeout
	// partway through Brian Otieno — his Patient/Visit/Examination rows existed but nothing past
	// that, and a name-only check silently treated him as fully seeded on the next run).
	alreadySeeded := func(fullName string) bool {
		p, err := client.Patient.Query().Where(patient.TenantID(t.ID), patient.FullName(fullName)).Only(ctx)
		if ent.IsNotFound(err) {
			return false
		}
		if err != nil {
			log.Fatalf("check existing patient %q: %v", fullName, err)
		}
		hasRx, err := client.Prescription.Query().Where(prescription.PatientID(p.ID)).Exist(ctx)
		if err != nil {
			log.Fatalf("check existing prescription for %q: %v", fullName, err)
		}
		if hasRx {
			log.Printf("%q already fully seeded for this tenant — skipping (idempotent re-run)", fullName)
		} else {
			log.Printf("%q exists but has no prescription yet — a prior run was likely interrupted; re-seeding requires manual cleanup of the partial patient/visit rows first (this script does not delete)", fullName)
		}
		return hasRx
	}

	// ── Patient A: full OPD golden path through a completed dispense ───────────────────────────
	if !alreadySeeded("Amina Hassan") {
		func() {
			patient, err := patientsSvc.RegisterPatient(ctx, t.ID, patients.RegisterPatientRequest{
				FullName: "Amina Hassan", Sex: "female", Phone: "0711222333", OutletID: out.ID,
			})
			if err != nil {
				log.Fatalf("[A] register patient: %v", err)
			}
			visit, err := patientsSvc.CheckInVisit(ctx, t.ID, patients.CheckInVisitRequest{
				PatientID: patient.ID, OutletID: out.ID, VisitType: "OPD", ChiefComplaint: "Fever and productive cough for 3 days",
			})
			if err != nil {
				log.Fatalf("[A] check in: %v", err)
			}
			if _, err := patientsSvc.RecordTriage(ctx, t.ID, patients.RecordTriageRequest{
				VisitID: visit.ID, TakenBy: nurseID,
				BPSystolic: ip(118), BPDiastolic: ip(76), TemperatureCelsius: fp(38.4),
				PulseBPM: ip(92), RespirationRate: ip(20), SpO2Percent: fp(97),
				Priority: "urgent", Notes: "Productive cough, mild respiratory distress",
			}); err != nil {
				log.Fatalf("[A] triage: %v", err)
			}
			exam, err := consultationSvc.RecordExamination(ctx, t.ID, consultation.RecordExaminationRequest{
				VisitID: visit.ID, ClinicianID: doctorID, QueueType: "doctor",
				ChiefComplaint: "Fever and productive cough", DiagnosisCode: "CA40", DiagnosisName: "Pneumonia, organism unspecified",
				TreatmentPlan: "Empirical antibiotics, review in 5 days if not improving", Notes: "Crepitations right lower zone",
			})
			if err != nil {
				log.Fatalf("[A] examination: %v", err)
			}
			if _, err := consultationSvc.CreateReferral(ctx, t.ID, consultation.CreateReferralRequest{
				VisitID: visit.ID, ReferredTo: "lab", Reason: "Rule out bacterial pneumonia", ReferredBy: doctorID,
			}); err != nil {
				log.Fatalf("[A] refer to lab: %v", err)
			}
			examID := exam.ID
			order, err := labSvc.CreateOrder(ctx, t.ID, lab.CreateOrderRequest{
				VisitID: visit.ID, ExaminationID: &examID, OrderedBy: doctorID, TestCodes: []string{"FBC"},
			})
			if err != nil {
				log.Fatalf("[A] create lab order: %v", err)
			}
			_, lines, err := labSvc.GetOrder(ctx, t.ID, order.ID)
			if err != nil {
				log.Fatalf("[A] get lab order: %v", err)
			}
			if _, err := labSvc.Collect(ctx, t.ID, lines[0].ID, lab.CollectRequest{CollectedBy: labTechID, SpecimenID: "SPEC-A-001"}); err != nil {
				log.Fatalf("[A] collect specimen: %v", err)
			}
			if _, err := labSvc.EnterResult(ctx, t.ID, lines[0].ID, lab.EnterResultRequest{
				ResultValue: "11.2", Unit: "g/dL", Flag: "normal", ResultedBy: labTechID,
			}); err != nil {
				log.Fatalf("[A] enter result: %v", err)
			}
			// Refer to pharmacy so the visit reaches "prescribed" before Dispense — without this,
			// nextVisitStatusAfterDispense's guard (only advances a visit sitting in "prescribed")
			// never fires and the visit stays stuck at "lab_complete" despite the prescription
			// itself being genuinely dispensed. Missing this step is exactly the bug this comment
			// exists to prevent recurring.
			if _, err := consultationSvc.CreateReferral(ctx, t.ID, consultation.CreateReferralRequest{
				VisitID: visit.ID, ReferredTo: "pharmacy", Reason: "Start antibiotics", ReferredBy: doctorID,
			}); err != nil {
				log.Fatalf("[A] refer to pharmacy: %v", err)
			}
			visitID := visit.ID
			patientID := patient.ID
			rx, err := pharmacySvc.CreatePrescription(ctx, t.ID, pharmacy.CreatePrescriptionRequest{
				OutletID: out.ID, PatientID: &patientID, VisitID: &visitID, ExaminationID: &examID,
				PrescriberName: "Dr. Achieng Otieno", PrescriberLicense: "KMPDC-12345", PatientName: patient.FullName,
				Lines: []pharmacy.PrescriptionLineInput{{
					DrugName: "Amoxicillin 500mg", Dosage: "1 capsule", Form: "capsule",
					Instructions: "1 capsule three times daily for 7 days", QuantityPrescribed: 21, UnitPrice: 15,
				}},
			})
			if err != nil {
				log.Fatalf("[A] create prescription: %v", err)
			}
			if _, err := pharmacySvc.ApprovePrescription(ctx, t.ID, rx.ID, pharmacistID, ""); err != nil {
				log.Fatalf("[A] approve prescription: %v", err)
			}
			if _, err := pharmacySvc.Dispense(ctx, t.ID, rx.ID, pharmacy.DispenseRequest{
				DispensedBy: pharmacistID, OutletID: out.ID, PatientName: patient.FullName,
				Lines: []pharmacy.DispenseLineInput{{LineID: rx.Edges.Lines[0].ID, QuantityToDispense: 21}},
			}); err != nil {
				log.Fatalf("[A] dispense: %v", err)
			}
			log.Printf("[A] Amina Hassan seeded — visit %s: registered -> triaged -> examined -> lab (normal) -> prescribed -> DISPENSED", visit.VisitNumber)
		}()
	}

	// ── Patient B: OPD with a CRITICAL lab result, prescription left pending (not dispensed) ──
	if !alreadySeeded("Brian Otieno") {
		func() {
			patient, err := patientsSvc.RegisterPatient(ctx, t.ID, patients.RegisterPatientRequest{
				FullName: "Brian Otieno", Sex: "male", Phone: "0722555666", OutletID: out.ID,
			})
			if err != nil {
				log.Fatalf("[B] register patient: %v", err)
			}
			visit, err := patientsSvc.CheckInVisit(ctx, t.ID, patients.CheckInVisitRequest{
				PatientID: patient.ID, OutletID: out.ID, VisitType: "OPD", ChiefComplaint: "High fever and chills for 2 days",
			})
			if err != nil {
				log.Fatalf("[B] check in: %v", err)
			}
			if _, err := patientsSvc.RecordTriage(ctx, t.ID, patients.RecordTriageRequest{
				VisitID: visit.ID, TakenBy: nurseID,
				BPSystolic: ip(100), BPDiastolic: ip(64), TemperatureCelsius: fp(39.6),
				PulseBPM: ip(110), RespirationRate: ip(24), SpO2Percent: fp(95),
				Priority: "emergency", Notes: "High-grade fever, rigors, looks unwell",
			}); err != nil {
				log.Fatalf("[B] triage: %v", err)
			}
			exam, err := consultationSvc.RecordExamination(ctx, t.ID, consultation.RecordExaminationRequest{
				VisitID: visit.ID, ClinicianID: doctorID, QueueType: "doctor",
				ChiefComplaint: "High fever and chills", DiagnosisCode: "1F43", DiagnosisName: "Malaria",
				TreatmentPlan: "Awaiting parasitology before starting antimalarials", Notes: "Clinically toxic, ?severe malaria",
			})
			if err != nil {
				log.Fatalf("[B] examination: %v", err)
			}
			if _, err := consultationSvc.CreateReferral(ctx, t.ID, consultation.CreateReferralRequest{
				VisitID: visit.ID, ReferredTo: "lab", Reason: "Confirm malaria parasitaemia", ReferredBy: doctorID,
			}); err != nil {
				log.Fatalf("[B] refer to lab: %v", err)
			}
			examID := exam.ID
			order, err := labSvc.CreateOrder(ctx, t.ID, lab.CreateOrderRequest{
				VisitID: visit.ID, ExaminationID: &examID, OrderedBy: doctorID, TestCodes: []string{"MPS"},
			})
			if err != nil {
				log.Fatalf("[B] create lab order: %v", err)
			}
			_, lines, err := labSvc.GetOrder(ctx, t.ID, order.ID)
			if err != nil {
				log.Fatalf("[B] get lab order: %v", err)
			}
			if _, err := labSvc.Collect(ctx, t.ID, lines[0].ID, lab.CollectRequest{CollectedBy: labTechID, SpecimenID: "SPEC-B-001"}); err != nil {
				log.Fatalf("[B] collect specimen: %v", err)
			}
			// CRITICAL flag — exercises the Sprint 3 per-line critical-result event
			// (events.EventLabOrderCriticalResult) and notifications-api's urgent SMS/push consumer.
			if _, err := labSvc.EnterResult(ctx, t.ID, lines[0].ID, lab.EnterResultRequest{
				ResultValue: "Plasmodium falciparum trophozoites seen, high parasitaemia (++++)", Unit: "", Flag: "critical", ResultedBy: labTechID,
			}); err != nil {
				log.Fatalf("[B] enter critical result: %v", err)
			}
			if _, err := consultationSvc.CreateReferral(ctx, t.ID, consultation.CreateReferralRequest{
				VisitID: visit.ID, ReferredTo: "pharmacy", Reason: "Start antimalarial treatment", ReferredBy: doctorID,
			}); err != nil {
				log.Fatalf("[B] refer to pharmacy: %v", err)
			}
			visitID := visit.ID
			patientID := patient.ID
			rx, err := pharmacySvc.CreatePrescription(ctx, t.ID, pharmacy.CreatePrescriptionRequest{
				OutletID: out.ID, PatientID: &patientID, VisitID: &visitID, ExaminationID: &examID,
				PrescriberName: "Dr. Achieng Otieno", PrescriberLicense: "KMPDC-12345", PatientName: patient.FullName,
				Lines: []pharmacy.PrescriptionLineInput{{
					DrugName: "Artemether-Lumefantrine 20/120mg", Dosage: "4 tablets", Form: "tablet",
					Instructions: "4 tablets twice daily for 3 days", QuantityPrescribed: 24, UnitPrice: 8,
				}},
			})
			if err != nil {
				log.Fatalf("[B] create prescription: %v", err)
			}
			if _, err := pharmacySvc.ApprovePrescription(ctx, t.ID, rx.ID, pharmacistID, ""); err != nil {
				log.Fatalf("[B] approve prescription: %v", err)
			}
			log.Printf("[B] Brian Otieno seeded — visit %s: registered -> triaged -> examined -> lab (CRITICAL) -> prescribed (pending dispense)", visit.VisitNumber)
		}()
	}

	// ── Patient C: active inpatient admission with vitals/ward-round/MAR data ──────────────────
	if !alreadySeeded("Grace Wambui") {
		func() {
			patient, err := patientsSvc.RegisterPatient(ctx, t.ID, patients.RegisterPatientRequest{
				FullName: "Grace Wambui", Sex: "female", Phone: "0733888999", OutletID: out.ID,
			})
			if err != nil {
				log.Fatalf("[C] register patient: %v", err)
			}
			visit, err := patientsSvc.CheckInVisit(ctx, t.ID, patients.CheckInVisitRequest{
				PatientID: patient.ID, OutletID: out.ID, VisitType: "OPD", ChiefComplaint: "Severe abdominal pain and vomiting",
			})
			if err != nil {
				log.Fatalf("[C] check in: %v", err)
			}
			if _, err := patientsSvc.RecordTriage(ctx, t.ID, patients.RecordTriageRequest{
				VisitID: visit.ID, TakenBy: nurseID,
				BPSystolic: ip(108), BPDiastolic: ip(68), TemperatureCelsius: fp(37.9),
				PulseBPM: ip(96), RespirationRate: ip(19), SpO2Percent: fp(98),
				Priority: "urgent", Notes: "Guarding on abdominal exam, dehydrated",
			}); err != nil {
				log.Fatalf("[C] triage: %v", err)
			}
			if _, err := consultationSvc.RecordExamination(ctx, t.ID, consultation.RecordExaminationRequest{
				VisitID: visit.ID, ClinicianID: doctorID, QueueType: "doctor",
				ChiefComplaint: "Severe abdominal pain and vomiting", DiagnosisCode: "DB90", DiagnosisName: "Acute gastroenteritis",
				TreatmentPlan: "Admit for IV fluids and observation", Notes: "Moderate dehydration, admitting for rehydration",
			}); err != nil {
				log.Fatalf("[C] examination: %v", err)
			}
			adm, err := inpatientSvc.Admit(ctx, t.ID, inpatient.AdmitRequest{
				VisitID: visit.ID, BedID: availableBed.ID, AdmittedBy: doctorID, IsolationPrecaution: "none",
			})
			if err != nil {
				log.Fatalf("[C] admit: %v", err)
			}
			if _, err := inpatientSvc.RecordVitalsChart(ctx, t.ID, inpatient.RecordVitalsChartRequest{
				AdmissionID: adm.ID, RecordedBy: nurseID,
				BPSystolic: ip(110), BPDiastolic: ip(70), TemperatureCelsius: fp(37.8),
				PulseBPM: ip(88), RespirationRate: ip(18), SpO2Percent: fp(98), PainScore: ip(4),
				Notes: "Improving, tolerating oral fluids, pain reduced with analgesia",
			}); err != nil {
				log.Fatalf("[C] vitals chart: %v", err)
			}
			if _, err := inpatientSvc.RecordWardRound(ctx, t.ID, inpatient.RecordWardRoundRequest{
				AdmissionID: adm.ID, ClinicianID: doctorID,
				Notes:         "Settling well on IV fluids, abdomen soft, bowel sounds present. Continue current management, review tomorrow.",
				DiagnosisCode: "DB90", DiagnosisName: "Acute gastroenteritis",
			}); err != nil {
				log.Fatalf("[C] ward round: %v", err)
			}
			visitID := visit.ID
			patientID := patient.ID
			rx, err := pharmacySvc.CreatePrescription(ctx, t.ID, pharmacy.CreatePrescriptionRequest{
				OutletID: out.ID, PatientID: &patientID, VisitID: &visitID,
				PrescriberName: "Dr. Achieng Otieno", PrescriberLicense: "KMPDC-12345", PatientName: patient.FullName,
				Lines: []pharmacy.PrescriptionLineInput{{
					DrugName: "IV Normal Saline 0.9% 1L", Dosage: "1 litre", Form: "IV infusion",
					Instructions: "Over 8 hours, reassess", QuantityPrescribed: 3, UnitPrice: 250,
				}},
			})
			if err != nil {
				log.Fatalf("[C] create prescription: %v", err)
			}
			if _, err := pharmacySvc.ApprovePrescription(ctx, t.ID, rx.ID, pharmacistID, ""); err != nil {
				log.Fatalf("[C] approve prescription: %v", err)
			}
			if _, err := marSvc.ChartDose(ctx, t.ID, mar.ChartDoseRequest{
				AdmissionID: adm.ID, PrescriptionLineID: rx.Edges.Lines[0].ID,
				Status: "given", AdministeredBy: nurseID, Notes: "First bag started, no adverse reaction",
			}); err != nil {
				log.Fatalf("[C] chart MAR dose: %v", err)
			}
			log.Printf("[C] Grace Wambui seeded — visit %s / admission %s in bed %s: ACTIVE admission with vitals, ward round, and a charted MAR dose", visit.VisitNumber, adm.ID, availableBed.BedNumber)
		}()
	}

	log.Println("seed-demo-workflow: complete — 3 demo patients seeded")
	fmt.Println("done")
}
