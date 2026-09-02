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

## Gap audit and MVP backlog candidates (2026-09-02)

**Shipped 2026-09-03** (see `docs/mvp-gap-backlog-2026-09-02.md`'s implementation pass): items 1
(`identification_type`), 2 (`sha_beneficiary_number`, auto-populated into `billing.Service.
CheckEligibility`'s fields map via the visit's patient), 3a (`Patient.photo_url` + a new
`POST /media/upload` endpoint), 4a (`patients.Service.CheckPossibleDuplicates`, a non-blocking
pre-registration lookup), and 6 (`ListVisits` now eager-loads each visit's latest `TriageRecord`
and sorts the registered/triaged bucket urgent-first) are all live, backend + hospital-ui. Also
shipped: `Patient.household_id` (schema-only, per item 5's own "not urgent" framing — no UI, still
nothing consumes it). **Still not built, as originally scoped**: 3b (fingerprint biometric —
explicitly out of scope), 4b (real patient-merge module — a genuinely separate follow-up), 5's UI
(family/household linkage has no consuming workflow yet), and 7 (appointment scheduling, unrelated
to this backlog).

Completeness audit of the shipped Patient registry and OPD queue against real-world HMIS practice
(KenyaEMR/OpenMRS primarily, plus fresh web research this session), done by reading the actual
shipped code, not just this sprint doc's own text. Every proposal below is marked **proposed, not
yet built**. Scope explicitly excludes referral/transfer/ambulance and the ward/bed/IPD sub-module,
both being audited in parallel this same session.

**Real doc-drift found by this audit, worth fixing on its own merits**: this sprint doc's own "Ent
Schemas to Add" section above still describes an `identification_type` enum (national_id/passport/
birth_certificate/maisha_number/alien_id). The actual shipped `Patient` schema
(`internal/ent/schema/patient.go`) has no such field, only a single free-text `id_number` commented
"National ID / passport". The matching hospital-ui sprint doc makes the same claim about a
"Maisha Number" selector, and the actual `patients/page.tsx` register form has one plain text input
and its own code comment already flags this drift ("that doc additionally describes a Kenya ID-type
selector... neither exists in `RegisterPatientInput`/`Patient` yet"). This section's proposals below
correct the doc against the code; the code itself is the gap.

1. **National ID / Maisha Number capture.** `compliance-kenya.md` §9-10 already confirms Maisha
   Number is a Client-Registry-accepted ID type. `Patient.id_number` is a single untyped string with
   no way to record which ID scheme it came from, so a facility cannot distinguish a Maisha Number
   from a passport or birth certificate in the data itself, only by convention. **Proposed**: add a
   nullable `Patient.identification_type` enum (`national_id`, `passport`, `birth_certificate`,
   `maisha_number`, `alien_id`), additive column, no backfill required (existing rows stay null/
   "unknown"). Small effort, mirrors an enum field this doc already described once. hospital-ui adds
   the matching selector next to the existing ID-number input.

2. **Insurance/payer capture at registration.** No field on `Patient` or any related entity captures
   an SHA/SHIF beneficiary number at registration. `Patient.client_registry_id` is the DHA Client
   Registry ID (a different identifier, reserved for the national HIE lookup per
   `docs/integrations.md` §2.4), not an SHA beneficiary number. Today, insurance identity is handled
   ad hoc and only at billing time, via `billing.Service.CheckEligibility`'s free-form
   `fields map[string]string` argument (`internal/modules/billing/service.go`), re-entered fresh at
   every eligibility check rather than captured once. This is now more consequential than it looks:
   fresh research this session found SHA ended OTP-based claim approval in August 2026 in favor of
   mandatory fingerprint biometric authentication for every claim (see item 3). **Proposed**: add a
   nullable `Patient.sha_beneficiary_number` field, captured at registration, auto-populated into the
   eligibility-check `fields` map so a returning patient's payer lookup never needs re-typing.

3. **Patient photo / biometric capture.** Zero fields, zero UI (`Patient` schema has no photo/
   biometric column; `patients/page.tsx` has no capture UI). This is no longer a pure nice-to-have:
   Kenya's SHA replaced OTP-based insurance-claim approval with mandatory fingerprint biometric
   verification in August 2026 (confirmed via web research, biometricupdate.com, "Kenya
   re-introduces biometric patient verification to curb insurance fraud"), rolled out at Level 4-6
   public facilities with Level 2-3 planned next. A facility billing SHA/SHIF through Codevertex Afya
   will eventually need SOME biometric touchpoint. **Proposed, staged**: (a) a simple nullable
   `Patient.photo_url` field + an upload step in the registration form, a low-effort visual-ID aid on
   its own merit (confirming the right chart at a busy front desk); (b) fingerprint biometric capture
   itself is a much larger integration (a physical scanner + SHA's own biometric API), explicitly
   **not proposed for this pass**. Flagged here so Sprint 12 (compliance hardening) or a dedicated
   SHA-integration effort scopes it deliberately rather than discovering it late.

4. **Duplicate-patient detection / merge.** `Patient.status`'s own schema comment already lists
   `merged` as a legal value ("active|inactive|merged"), but no code anywhere sets it or performs a
   merge. `patients.Service` has zero duplicate-check logic; `RegisterPatient` will happily create a
   second MRN for the same person typed with a slightly different name or phone. This is a
   well-documented real-world HMIS pain point. OpenMRS's own ecosystem treats it as a real module
   (`openmrs-module-patientmatching`, a full patient-merge UI, deliberately refusing to merge patients
   with existing orders because that case is genuinely hard). This is not a case where a five-line
   fix closes the gap. **Proposed, staged**: (a) quick win, a "possible duplicate" warning at
   registration time, a pre-save query against the existing `tenant_id, phone` / `tenant_id,
   id_number` / `tenant_id, full_name` indexes already on `Patient` (no schema change, these indexes
   already exist), surfaced as a non-blocking confirm-anyway prompt; (b) a real `MergePatients`
   service method that reassigns visits/accounts/prescriptions from a duplicate record to a survivor
   and sets the duplicate's status to `merged` is a genuinely new, more involved module, scope it as
   its own follow-up rather than bundling it with (a).

5. **Family / household linkage.** No field anywhere links one `Patient` to another (confirmed:
   no `household_id`/`family_id` column on any schema). SHA's own registration model already links a
   spouse and children under 18 to one beneficiary profile (marriage certificate / birth certificate
   required, confirmed via web research this session), and Sprint 10's planned ANC/PNC/pediatric
   programmes will eventually want mother-child linkage. **Proposed, deferred by design**: a
   lightweight nullable `Patient.household_id` (self-referencing UUID, pointing at a "head of
   household" Patient row) is enough to avoid a retrofit later, additive and no migration risk to add
   now even though nothing consumes it until Sprint 10. Flagging now, per this audit's own brief, so a
   future sprint doesn't have to bolt this onto an already-large `Patient` table under time pressure.
   **Not proposed as urgent**: no current workflow needs it.

6. **OPD queue acuity-based reordering.** `patients.Service.ListVisits` (`internal/modules/patients/
   service.go`) orders the OPD/consultation queue strictly `ent.Asc(patientvisit.FieldCreatedAt)`,
   pure FIFO by check-in time. `TriageRecord.priority` (an ESI-style 1-5 acuity field, per the
   schema's own comment) is captured and stored, but nothing ever reads it back into queue ordering
   or the consultation worklist. It sits on the triage record, visible only if a clinician opens
   that specific patient's chart. Confirmed via web research: acuity-based queue reordering (the
   Emergency Severity Index model) is the dominant real-world triage-to-provider pattern, not an
   edge case. **Proposed**: no new field needed, the data already exists. `ListVisits` gains an
   optional join to each visit's latest `TriageRecord.priority` and sorts urgent-first within the
   same status bucket (registered/triaged visits only; a visit already `in_examination` or beyond
   doesn't need re-sorting). hospital-ui's consultation queue shows a priority badge and reflects the
   new order. Low effort. A quick win precisely because the acuity data already exists and only the
   read-side ordering is missing.

7. **Appointment scheduling.** Confirmed still not built: `hospital-ui`'s `Appointments` nav entry
   (`lib/nav-config.ts`) is `comingSoon: true` with zero backend route behind it. This matches the
   already-tracked, already-acknowledged gap in `docs/plan.md`'s Core Capabilities list. No further
   research done here per this audit's own scope (deep appointment-system research was explicitly
   out of scope for this pass); noted only to confirm the state hasn't silently changed.

See `hospital-api/docs/mvp-gap-backlog-2026-09-02.md` for this item's place in the full
sprint-by-sprint backlog.
