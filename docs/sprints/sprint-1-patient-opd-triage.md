# Hospital API — Sprint 1: Patient Registry, OPD Reception & Queuing, Triage

**Status:** ⏳ Planned
**Depends on:** Sprint 0 (Foundations)
**Goal:** Stand up the spine every other module hangs off — `Patient`, `PatientVisit`, `TriageRecord` — migrated in *meaning* from pos-api's existing schemas (not copy-pasted; fix known issues along the way, e.g. adopt `shared/service-client` instead of a hand-rolled HTTP client).

## Context

Every subsequent sprint (Consultation, Lab, Pharmacy, Inpatient) attaches to a `PatientVisit`. This
sprint must ship first and correctly, since everything else assumes it exists. Reference pos-api's
existing `patient.go`/`patientvisit.go`/`triagerecord.go` ent schemas for the field list, but do not
copy pos-api's data-ownership mistakes forward.

## Ent Schemas to Add

- `patient` — `tenant_id`, `mrn` (unique per tenant, sequence-allocated), `full_name`, `dob`, `sex`, `phone`, `next_of_kin` (free-text quick-reference field for the chart — NOT the same as Sprint 5's dedicated `PatientNextOfKin` entity, which is the structured, ID-numbered record used to authorize a bill settlement/discharge; keep both, different purposes), `crm_contact_id` (nullable), timestamps. Retention: no hard-delete ever (Kenya DPA 20-year minimum) — soft status only.
- `patient_visit` — `tenant_id`, `patient_id`, `outlet_id`, `visit_type` (OPD/IPD), `status`, `checked_in_at`, `discharged_at`. On check-in, post a `BillableCharge` for the visit's registration fee via Sprint 5's billing primitive (`applies_to: first_visit|return_visit` resolved from whether the patient has a prior settled visit) — registration/records defaults to the Billing-desk `billing_queue` collection mode per `docs/architecture.md`'s facility-tier table, so this does not require records staff to hold a collection permission by default.
- `triage_record` — `patient_visit_id`, `vitals` (BP/temp/pulse/weight/SpO2 as JSON or discrete columns — decide based on reporting needs), `priority` (ESI-style acuity level), `recorded_by`.
- `referral` — `patient_visit_id`, `referred_to`, `reason`, `status`.
- Global reference tables from `erd.md` if not already seeded: none required yet (role/permission catalogue is Sprint 1's RBAC work, see below).
- `hospital_role`/`hospital_permission`/`role_permission`/`hospital_user`/`user_role_assignment` (Trinity Layer 3 local RBAC, global role tables per `feedback_shared_core_reference_data.md` — no `tenant_id` on the role/permission tables themselves).

## Endpoints

- `POST /{tenant}/hospital/patients` — register patient (generates MRN via a sequence allocator, mirroring `library-api`'s `modules/sequence` pattern).
- `GET /{tenant}/hospital/patients`, `GET /{tenant}/hospital/patients/{id}` — search/detail.
- `POST /{tenant}/hospital/visits` — check in / start a visit.
- `GET /{tenant}/hospital/visits?status=` — OPD queue.
- `POST /{tenant}/hospital/visits/{id}/triage` — record vitals + priority.
- `GET /{tenant}/hospital/auth/me` — Trinity Layer 3 endpoint (service role + `hospital.*.*` permissions), matching every sibling service's convention.

## Integration Points

- auth-api JIT provisioning + default role assignment on first request (mirror `library-api`'s `EnsureUserFromToken` pattern).
- Publish `hospital.patient.created`, `hospital.visit.admitted` via the outbox (first real use of `shared-events` in this service — wire the `outboxevent` ent schema + `OutboxPoller` here, since this is the first sprint with a real ent client).

## Definition of Done

- [ ] `go generate ./internal/ent/...` + Atlas versioned migration generated and committed.
- [ ] `go build ./...` / `go vet ./...` clean.
- [ ] Patient registration → visit check-in → triage happy path works end to end (manual curl or integration test).
- [ ] `GET /{tenant}/hospital/auth/me` returns a JIT-provisioned default role.
- [ ] Outbox events publish and are visible on the `hospital` NATS stream.
- [ ] Docs updated: `erd.md`/`architecture.md` marked "implemented" for these tables.

## Next Sprint

Sprint 2 — Consultation & Examination, Diagnosis Catalogue.
