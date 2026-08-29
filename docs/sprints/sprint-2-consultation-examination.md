# Hospital API — Sprint 2: Consultation & Examination

**Status:** ⏳ Planned
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
- `POST /{tenant}/hospital/visits/{id}/refer` — create a `Referral` (schema already added in Sprint 1) to lab or pharmacy or another facility.

## Integration Points

- No new external service calls this sprint — purely internal. Sets up the `diagnosis_id` FK that
  Lab (Sprint 3) and Billing (Sprint 5) will reference.

## Definition of Done

- [ ] Global diagnosis catalogue seeded idempotently at startup (mirror `library-api`'s
      `refdata.SeedGlobal*` pattern — a `modules/refdata` package, not ad-hoc seed code scattered
      across handlers).
- [ ] Consultation → diagnosis → referral-to-lab happy path works end to end.
- [ ] Atlas migration generated and committed.
- [ ] `go build`/`go vet` clean.

## Next Sprint

Sprint 3 — Laboratory.
