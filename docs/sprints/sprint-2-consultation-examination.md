# Hospital API — Sprint 2: Consultation & Examination

**Status:** ✅ Shipped 2026-08-29 (`hospital-api@709b140`) — `ExaminationRecord`,
`DiagnosisCatalogDefault` (global, confirmed ICD-11, a 20-code starter set)/`DiagnosisCatalogEntry`
(tenant-custom), the new `internal/modules/refdata` seed package, and every endpoint below.
Build/vet/test green; no live E2E walkthrough run yet (see the master migration plan's Known Gaps).
**Depends on:** Sprint 1 (Patient/Visit/Triage)
**Goal:** Doctor/dental/MCH/specialist consultation workflow with structured examination notes and diagnosis capture.

## Context

Builds directly on `PatientVisit` from Sprint 1. Introduces the first **global reference data** table
in this service (`diagnosis_catalog_default`) — per `feedback_shared_core_reference_data.md`, the
standard diagnosis catalogue is identical for every tenant and must NOT carry `tenant_id`; only
tenant-custom additions do.

## Ent Schemas to Add

- `examination_record` — `patient_visit_id`, `clinician_id` (references `auth_service_user_id`), `notes`, `diagnosis_id`, `queue_type` (doctor/dental/MCH/specialist).
- `diagnosis_catalog_default` — global, seeded once (`code`, `name`, `category`) — no `tenant_id`.
- `diagnosis_catalog_entry` — `tenant_id` (nullable — null rows are the seeded defaults, non-null are tenant-custom), `code`, `name`.

## Endpoints

- `POST /{tenant}/hospital/visits/{id}/examination` — record consultation notes + diagnosis. Posts a `BillableCharge` for the visit's consultation fee (Sprint 5) at creation — `collection_mode` defaults to `billing_queue` for Clinic-tier, `direct` for Facility/Hospital tier (see `docs/architecture.md`'s facility-tier defaults table).
- `GET /{tenant}/hospital/diagnosis-catalog` — merged global + tenant-custom list.
- `POST /{tenant}/hospital/diagnosis-catalog` — add a tenant-custom entry.
- `POST /{tenant}/hospital/visits/{id}/refer` — create a `Referral` (schema already added in Sprint 1) to lab or pharmacy or another facility. **Note (2026-09-02, planned, not this sprint's scope):** "or another facility" is currently just the free-string `referred_to: "external_facility"` value — a real inter-facility referral (letter content, receiving-facility identity, pre-transfer contact confirmation, counter-referral feedback) is a richer, additive shape designed in `docs/erd.md`/`docs/architecture.md`, landing whenever that work is scheduled (likely alongside or after Sprint 6/9, since it also touches `PatientTransfer` and `AmbulanceBooking`), not shipped by this sprint.

## Integration Points

- No new external service calls this sprint — purely internal. Sets up the `diagnosis_id` FK that
  Lab (Sprint 3) and Billing (Sprint 5) will reference.

## Definition of Done

- [x] Global diagnosis catalogue seeded idempotently at startup (mirror `library-api`'s
      `refdata.SeedGlobal*` pattern — a `modules/refdata` package, not ad-hoc seed code scattered
      across handlers). Explicitly a 20-code starter set, not a full ICD-11 import — `cmd/seed/main.go`
      was also found to be a no-op stub and fixed to actually run it.
- [ ] Consultation → diagnosis → referral-to-lab happy path works end to end — **not yet run** against
      a live server this session, see the master plan's Known Gaps.
- [x] Atlas migration generated and committed.
- [x] `go build`/`go vet` clean.

## Next Sprint

Sprint 3 — Laboratory.
