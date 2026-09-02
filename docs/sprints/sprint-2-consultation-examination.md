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

## Gap audit and MVP backlog candidates (2026-09-02)

Completeness audit of the shipped Consultation/Examination module against what a real examination
record captures in production HMIS systems, done against the actual shipped
`internal/ent/schema/examination_record.go` and `internal/modules/consultation/service.go`, not
this sprint doc's own text. All proposals below are **proposed, not yet built**. Referral/transfer
content is explicitly out of scope here, covered by a parallel audit this session.

1. **Vitals at consultation vs. triage-only.** `ExaminationRecord` has no vitals fields of its own;
   the consultation queue relies entirely on whatever `TriageRecord` was captured earlier in the
   visit. The consultation queue page's own UI copy confirms this design as intentional today
   ("Visits appear here once vitals are recorded in Triage"). The underlying schema already supports
   a re-triage: `TriageRecord`'s own doc comment states "a visit may be re-triaged... rows are
   append-only, the latest by `taken_at` is authoritative," so the data model is fine, only the
   workflow trigger is missing. A patient who waited a long time between triage and being seen has no
   prompt to recheck vitals before the doctor writes a diagnosis. **Proposed**: no schema change
   needed for the minimum fix, wire a "Recheck vitals" action into the consultation UI that creates a
   new `TriageRecord` inline (the capability already exists, `patients.Service.RecordTriage` is
   already callable from any pre-terminal visit state). A further, more invasive option, a nullable
   `ExaminationRecord.vitals_snapshot JSON` field that freezes exactly which vitals reading the
   clinician reviewed at diagnosis time for audit purposes, is noted as a lower-priority follow-on,
   not proposed for this pass.

2. **Structured review-of-systems / physical exam findings.** `ExaminationRecord.notes` is a single
   free-text `String` field; there is no discrete review-of-systems or per-system physical-exam
   structure anywhere in the schema. Real production EMRs typically capture at least a
   system-by-system findings structure (cardiovascular, respiratory, abdominal, etc.) separately from
   a free-text narrative, both for clinical completeness and because a structured field is what makes
   later decision-support/quality-reporting possible. **Proposed**: two additive nullable JSON fields,
   `review_of_systems` and `physical_exam_findings` (both a simple `map[string]string` keyed by body
   system), sitting alongside the existing `notes` field rather than replacing it. The schema change
   itself is small; the real cost is a structured-form UI build on the hospital-ui side, flagged as
   the larger half of this item.

3. **Provisional vs. final diagnosis distinction.** `ExaminationRecord` has exactly one
   `diagnosis_code`/`diagnosis_name` pair. The existing status lifecycle (`in_progress` →
   `awaiting_lab` → `completed`, with `lab.Service.EnterResult` reopening a record to `in_progress`
   once results return) already gives the WORKFLOW effect of "revisit the diagnosis after labs come
   back," but the diagnosis fields themselves are overwritten in place with no record of what the
   original working diagnosis was. **Proposed**: rather than a full provisional/final field split
   (a larger, more invasive change touching every diagnosis-reading call site), an additive
   `diagnosis_history JSON` array field that appends `{code, name, changed_by, changed_at}` on every
   diagnosis write gives basic auditability at much lower cost, while `diagnosis_code`/`diagnosis_name`
   keep meaning "the current, latest diagnosis" exactly as today.

4. **Treatment plan / orders section.** Consultation today only produces two outcomes: a referral
   (lab/pharmacy/another facility) or a plain-text note. There is no discrete field for "advice given,
   no referral needed" (e.g. "rest, hydrate, follow up in 3 days if not improved"), a genuinely common
   real encounter outcome that currently has nowhere structured to live except inside the free-text
   `notes` field, indistinguishable from any other note. **Proposed**: an additive nullable
   `ExaminationRecord.treatment_plan` text field, separate from `notes`, specifically for this
   no-referral-needed case. Small, additive, no migration risk.

See `hospital-api/docs/mvp-gap-backlog-2026-09-02.md` for this item's place in the full
sprint-by-sprint backlog.
