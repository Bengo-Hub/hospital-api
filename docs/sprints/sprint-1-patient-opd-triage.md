# Hospital API — Sprint 1: Patient Registry, OPD Reception & Queuing, Triage

**Status:** ✅ Shipped 2026-08-29 (`hospital-api@05741fd`) — `Patient`/`PatientVisit`/`TriageRecord`/
`Referral` schemas, sequence-based MRN/visit-number allocation (ported `internal/modules/sequence`
from library-api), the first real transactional-outbox wiring (`OutboxEvent` + `shared-events`
`OutboxPoller`), and every endpoint below. Build/vet/test green; no live E2E walkthrough run yet
(see the master migration plan's Known Gaps).
**Depends on:** Sprint 0 (Foundations)
**Goal:** Stand up the spine every other module hangs off — `Patient`, `PatientVisit`, `TriageRecord` — migrated in *meaning* from pos-api's existing schemas (not copy-pasted; fix known issues along the way, e.g. adopt `shared/service-client` instead of a hand-rolled HTTP client).

## Context

Every subsequent sprint (Consultation, Lab, Pharmacy, Inpatient) attaches to a `PatientVisit`. This
sprint must ship first and correctly, since everything else assumes it exists. Reference pos-api's
existing `patient.go`/`patientvisit.go`/`triagerecord.go` ent schemas for the field list, but do not
copy pos-api's data-ownership mistakes forward.

## Ent Schemas to Add

- `patient` — `tenant_id`, `mrn` (unique per tenant, sequence-allocated), `full_name`, `dob`, `sex`, `phone`, `next_of_kin` (free-text quick-reference field for the chart — NOT the same as Sprint 5's dedicated `PatientNextOfKin` entity, which is the structured, ID-numbered record used to authorize a bill settlement/discharge; keep both, different purposes), `crm_contact_id` (nullable), `identification_type` (`national_id`/`passport`/`birth_certificate`/`maisha_number`/`alien_id` — matches the ID types Kenya's own national Client Registry accepts, see `docs/compliance-kenya.md` §6), `identification_number`, timestamps. Retention: no hard-delete ever (Kenya DPA 20-year minimum) — soft status only.
- `patient_visit` — `tenant_id`, `patient_id`, `outlet_id`, `visit_type` (OPD/IPD), `status`, `checked_in_at`, `discharged_at`. On check-in, post a `BillableCharge` for the visit's registration fee via Sprint 5's billing primitive (`applies_to: first_visit|return_visit` resolved from whether the patient has a prior settled visit) — registration/records defaults to the Billing-desk `billing_queue` collection mode per `docs/architecture.md`'s facility-tier table, so this does not require records staff to hold a collection permission by default.
- `triage_record` — `patient_visit_id`, `vitals` (BP/temp/pulse/weight/SpO2 as JSON or discrete columns — decide based on reporting needs), `priority` (ESI-style acuity level), `recorded_by`.
- `referral` — `patient_visit_id`, `referred_to`, `reason`, `status`. Field shape kept deliberately simple enough to map onto FHIR `ServiceRequest`/`Task` later, since Kenya's own community-health system (eCHIS) and a draft national e-Referral FHIR Implementation Guide are both heading that direction (`docs/integrations.md` §2D) — no integration work is scheduled for this sprint, just avoid a shape that would need a rewrite later. **Note (2026-09-02, planned, not this sprint's scope):** this shape is enough for the internal department hand-off this sprint actually needs, but not for a genuine inter-facility referral. The richer fields (`referral_type`, `referral_summary`, receiving-facility identity, counter-referral) are designed in `docs/erd.md` and `docs/architecture.md`'s "Referral, Transfer & Ambulance Billing" section, as an additive migration once that work is scheduled — no rewrite of this sprint's shipped columns is implied.
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

- [x] `go generate ./internal/ent/...` + Atlas versioned migration generated and committed.
- [x] `go build ./...` / `go vet ./...` clean.
- [ ] Patient registration → visit check-in → triage happy path works end to end (manual curl or integration test) — **not yet run**: no request has been fired against a running server this session, see the master plan's Known Gaps. This is Phase 8's job.
- [x] `GET /{tenant}/hospital/auth/me` returns a JIT-provisioned default role (shipped as part of the 2026-08-01 Trinity wiring, exercised by every sprint since).
- [ ] Outbox events publish and are visible on the `hospital` NATS stream — the publisher/poller is wired and unit-tested at the code level, but not yet observed against a live NATS stream this session.
- [x] Docs updated: `erd.md`/`architecture.md` marked "implemented" for these tables.

## Next Sprint

Sprint 2 — Consultation & Examination, Diagnosis Catalogue.
