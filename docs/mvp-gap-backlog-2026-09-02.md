# MVP Gap Backlog (2026-09-02)

A single, scannable index of every completeness gap found across three parallel audit passes run
this session against hospital-service's already-shipped sprints, organized sprint by sprint so a
future session can work through it methodically once the current MVP push is done. Nothing in this
document changes shipped behaviour: every row is a **proposed** candidate for a future session,
cross-referenced to the fuller detail written into the relevant sprint doc.

This document does not replace the sprint docs. It is an index into them. Read the linked section
for the actual reasoning, code citations, and research sources behind each row.

## How this doc came together

Three audits ran in parallel this session, each covering different territory to avoid duplication.

1. **This audit** (documented here): Sprint 1 (Patient/OPD/Triage, minus referral), Sprint 2
   (Consultation/Examination), Sprint 3 (Laboratory), Sprint 4 (Pharmacy/Dispensing), Sprint 5
   (Billing/Insurance), and the User Management/RBAC module.
2. A parallel audit of Sprint 6 (Inpatient) and Sprint 7 (Theatre/ICU). See
   `docs/sprints/sprint-6-inpatient.md`'s and `docs/sprints/sprint-7-theatre-icu.md`'s own "Gap
   audit" sections, written by that session.
3. A parallel audit of referral/transfer/ambulance workflows across every sprint they touch. See
   whichever doc that session landed its findings in, likely `docs/architecture.md`'s "Referral,
   Transfer & Ambulance Billing" section and/or the relevant sprint docs.

Audits (2) and (3) have since finished — their findings are summarized below rather than left as
placeholders, once it was safe to read their output without risking a write collision.

## Sprint 1: Patient Registry, OPD Reception & Queuing, Triage

Full detail: `docs/sprints/sprint-1-patient-opd-triage.md` section "Gap audit and MVP backlog
candidates (2026-09-02)" (backend), `hospital-ui/docs/sprints/sprint-1-reception-opd-triage.md`'s
matching section (frontend).

| Gap | Effort | Notes |
|---|---|---|
| `identification_type` enum missing on `Patient` (national_id/passport/birth_certificate/maisha_number/alien_id) | Quick, additive field | **Shipped 2026-09-03.** This sprint's own doc already described this field; it was never actually built. Doc/code drift, corrected by this audit. |
| No SHA/SHIF beneficiary number captured at registration | Quick, additive field | **Shipped 2026-09-03.** Auto-populated into `billing.Service.CheckEligibility`'s fields map via the visit's patient. |
| No patient photo/biometric capture | Quick (photo), large (fingerprint) | **Photo shipped 2026-09-03** (`Patient.photo_url` + new `POST /media/upload`). SHA moved to mandatory fingerprint biometric claim approval in Aug 2026 — fingerprint remains a real hardware and API integration, explicitly not scoped this pass. |
| No duplicate-patient detection or merge, despite `Patient.status` already listing `merged` as a legal value | Quick (duplicate warning), new module (real merge) | **Duplicate warning shipped 2026-09-03** (`CheckPossibleDuplicates` + a non-blocking UI confirm). Real merge (reassigning visits/prescriptions) remains its own, larger, not-yet-built follow-up, as OpenMRS treats it as a full module in its own right. |
| No family/household linkage | Deferred, not urgent | `Patient.household_id` schema field shipped 2026-09-03 (no UI, nothing consumes it yet) so Sprint 10 (ANC/PNC) doesn't have to retrofit it later. |
| OPD/consultation queue is pure FIFO by check-in time; `TriageRecord.priority` (ESI-style acuity) is captured but never read back into queue ordering | Quick, query and UI change, no schema change | **Shipped 2026-09-03.** `ListVisits` sorts the registered/triaged bucket urgent-first; the consultation queue shows a priority badge. |
| Appointment scheduling | N/A | Confirmed still `comingSoon` with zero backend. Already tracked, not re-researched this pass. |

## Sprint 2: Consultation & Examination

Full detail: `docs/sprints/sprint-2-consultation-examination.md` section "Gap audit and MVP backlog
candidates (2026-09-02)" (backend), `hospital-ui/docs/sprints/sprint-2-consultation-examination.md`'s
matching section (frontend).

| Gap | Effort | Notes |
|---|---|---|
| No vitals recheck workflow at consultation time (relies entirely on triage-time vitals) | Quick, UI wiring only | **Shipped 2026-09-03.** `TriageModal` extracted to a shared component, opened inline from a new "Recheck vitals" button in the examination modal. |
| No structured review-of-systems / physical-exam-by-system fields (`notes` is free text only) | Moderate, small schema plus real form-building UI | **Shipped 2026-09-03.** Additive JSON fields + a per-body-system `SystemsGrid` UI for both. |
| No provisional-vs-final diagnosis distinction; diagnosis fields overwrite in place | Quick, additive `diagnosis_history` log | **Shipped 2026-09-03.** Appends on every diagnosis-changing write; a new `GET .../examination` endpoint + UI trail line shows it. |
| No discrete treatment-plan/no-referral-needed field | Quick, additive field | **Shipped 2026-09-03.** Additive `treatment_plan` field + a dedicated textarea. |

## Sprint 3: Laboratory

Full detail: `docs/sprints/sprint-3-laboratory.md` section "Gap audit and MVP backlog candidates
(2026-09-02)" (backend), `hospital-ui/docs/sprints/sprint-3-laboratory.md`'s matching section
(frontend).

| Gap | Effort | Notes |
|---|---|---|
| No specimen collection tracking (collector, timestamp, specimen ID). The one enum value that gestured at this (`collected`) was removed as confirmed dead code | Moderate, additive fields plus new worklist step | **Shipped 2026-09-03.** New fields + `POST .../lines/{lineID}/collect`; `EnterResult` now hard-gates on collection. |
| No critical-value alerting distinct from routine "results ready". `LabOrderLine.flag=critical` has no downstream effect today | Small to moderate, new event plus notifications-api consumer | **Shipped 2026-09-03.** New `hospital.lab_order.critical_result` event (per-line, immediate) + a notifications-api consumer sending an urgent SMS/push to the ordering clinician. |
| Referred-out/external lab tracking | N/A | Explicitly out of scope for this audit, covered elsewhere per this sprint doc's own section 2E note. |

## Sprint 4: Pharmacy & Dispensing

Full detail: `docs/sprints/sprint-4-pharmacy-dispensing.md` section "Gap audit and MVP backlog
candidates (2026-09-02)" (backend), `hospital-ui/docs/sprints/sprint-4-pharmacy-dispensing.md`'s
matching section (frontend).

| Gap | Effort | Notes |
|---|---|---|
| No Medication Administration Record (MAR) for an inpatient. Dispense is tracked; per-dose nurse administration is not | New small module | **Shipped 2026-09-03** as an on-demand "chart a dose" screen (no dosing-frequency data model exists to pre-populate a schedule from) on the admission detail page — see the sprint doc's own note. |
| No prescription refill/repeat workflow for chronic medication | Quick, additive field plus one new method | **Shipped 2026-09-03.** |
| Allergy recheck doesn't fire automatically when `Patient.allergy_flags` changes | Quick, wiring only, backend-only | **Shipped 2026-09-03.** |

## Sprint 5: Billing & Insurance

Full detail: `docs/sprints/sprint-5-billing-insurance.md` section "Gap audit and MVP backlog
candidates (2026-09-02)" (backend), `hospital-ui/docs/sprints/sprint-5-billing-insurance.md`'s
matching section (frontend).

| Gap | Effort | Notes |
|---|---|---|
| No deposit collection at admission time (only a discharge-time balance gate exists) | Quick, mirrors an existing pattern | **Shipped 2026-09-03.** New `ADMISSION_DEPOSIT` catalog code + best-effort charge in `Admit`, plus `insurance_guarantee_reference` for the insured path. |
| No printed/PDF itemized invoice or receipt anywhere in hospital-api/hospital-ui | Moderate, cross-service | **Shipped 2026-09-03.** Confirmed treasury-api's S2S PDF route already existed — zero treasury-api changes needed, only a thin hospital-api proxy + UI download link. |
| No refund/credit-note workflow for an overpayment or billing error | Quick, the hard part already exists | **Shipped 2026-09-03.** |
| Price-list versioning | No action needed | Confirmed already correct: `BillableCharge.amount` is snapshotted at post time, so an already-open account is unaffected by a later catalog price change. |

## User Management / RBAC module (cross-cutting, no sprint-numbered doc)

This module isn't tracked as a numbered sprint. It lives in `docs/architecture.md`'s "User
Management Module" section and `d:\Projects\Codevertex\.claude\memory\
hospital-user-management-rebuild-2026-08-30.md`, and `architecture.md` is being edited by a
parallel session this same window, so the full write-up below lives here instead of risking a write
collision. Read the 2026-08-30 rebuild memory file first: it already closed the multi-tenant-identity
bug, added copy-on-write role customization, `RbacAuditLog`, deactivation enforcement, `expires_at`
enforcement, a real invite flow, per-user outlet enforcement, role deletion, and facility config
settings. This audit only looked for what's still missing beyond that already-substantial rebuild.

**Shift/duty-roster integration.** Confirmed explicitly out of scope, unchanged. `docs/plan.md`'s
"Explicitly out of scope for Codevertex Afya v1" section already states duty rostering is owned by
`erp-api`. Not re-researched this pass, per this audit's own brief.

**Professional license / registration number for clinical staff.** `HospitalUser`
(`internal/ent/schema/hospital_user.go`) has no license/registration field at all, only email,
name, status, and sync metadata. `Prescription.prescriber_license` does exist, but it is a
free-text field populated only for an external prescriber on a walk-in/chemist dispense (per the
schema's own comment: "Set when dispensing against a prescription written elsewhere"), not a
structured, verified field on a facility's own internal staff record. `compliance-kenya.md`
discusses KMPDC exclusively at the facility level (Certificate of Data Handler/Processor, MFL code,
facility KMPDC registration number as tenant metadata), never at the individual-practitioner level.
Kenya's real regulatory practice requires annual individual licensing for doctors (KMPDC), nurses
(Nursing Council of Kenya), and pharmacists (Pharmacy and Poisons Board), and a clinical document
(prescription, lab request) issued by an internal staff member today carries no practitioner
registration number at all. **Shipped 2026-09-03**: additive nullable `HospitalUser.
professional_registration_number`/`professional_registration_body` fields, a new
`identity.Service.UpdateUserProfile` + `PUT /users/{userID}/professional-registration` route,
surfaced on the Users admin page (`ProfessionalRegistrationCell`), and threaded automatically into
`CreatePrescriptionRequest.PrescriberLicense` when an internal clinician (not an external/chemist
walk-in) creates a prescription with no license already supplied. **Note**: not wired into
`InviteMemberModal` as originally sketched — a `HospitalUser` row doesn't exist until a staff
member's first real sign-in (JIT-provisioned then, per `EnsureUserFromToken`), so there is nowhere
to persist these fields at invite time; the Users-page edit action is the only viable surface.

**Session/audit trail for clinical actions specifically.** Confirmed: `RbacAuditLog` is deliberately
scoped, per its own schema doc comment, to identity/RBAC mutations only (role assigned/changed, role
created/customized, user status changed). Grep of `auditlog.Writer`'s call sites confirms it is
invoked only from `identity.Service` and `rbac.Service`, never from patients/consultation/lab/
pharmacy/billing. This is not a fresh discovery: the 2026-08-30 rebuild memory already states this
gap is deliberate ("Sprint 12's full compliance-grade `audit_log`/`consent_record`... deliberately
named/scoped narrower and apart from this, so the two can coexist... whenever Sprint 12 actually
gets built"). Flagging here only to confirm the structural gap is real today (zero clinical-record
view/edit audit trail exists), so Sprint 12's own sizing accounts for it accurately. **Won't fix in
this pass**, since it's already deliberately deferred, not a new proposal.

| Gap | Effort | Notes |
|---|---|---|
| No professional license/registration number field for internal clinical staff | Quick, additive fields | **Shipped 2026-09-03.** |
| No clinical-record audit trail (patient/exam/lab/prescription view or edit) | Deferred by design | Already acknowledged in the 2026-08-30 rebuild memory as Sprint 12 scope; not a new finding, flagged for sizing accuracy only. |
| Shift/duty-roster integration | Out of scope | Confirmed owned by `erp-api`, unchanged. |

## Sprint 6: Inpatient

Full detail: `docs/sprints/sprint-6-inpatient.md`'s "Gap audit and Sprint 6.1 candidates" section,
written by a parallel session running concurrently with this one.

| Gap | Effort | Notes |
|---|---|---|
| No ward/bed type (General/Private/Semi-Private/Isolation/ICU) distinct from the free-form `billable_item_code` already on `Ward` | Quick, additive `ward_type` enum | Would default a sensible `billable_item_code` per type without removing the existing override. |
| No isolation-precaution flag on a bed/admission | Quick, additive enum | CDC transmission-based-precaution categories (contact/droplet/airborne/none), modeled per-bed since isolation is a stay state, not a fixed ward classification. |
| `Admission.discharge_summary` is a single free-text field, no structured content | Moderate, additive fields | Joint Commission-style structured discharge summary (diagnosis, procedures, discharge medications, follow-up, condition at discharge), free text kept as an "additional notes" field. |
| No nursing vitals/ward-round tracking during an inpatient stay, distinct from Triage (OPD-only) | New small module | A `vitals_chart_entry`/`ward_round_note` entity tied to an admission, most naturally on the admission detail page. |
| Transfer history not visible in hospital-ui despite `PatientTransfer` rows existing and being used for billing | Quick, UI only | Already flagged in Sprint 6's own DoD as a known gap, not new to this pass. |
| **Biomedical Equipment / Asset integration** — shipped this session | Done | Originally this section's headline finding; implemented for real (`Bed.equipment_asset_ids`, a read-only `/assets` page). See `docs/architecture.md`'s "Biomedical Equipment / Asset Integration" section. Two real gaps in inventory-api's `AssetReservation` (no overlap check, no status-transition endpoint) were found and flagged back rather than fixed, since they block real equipment-conflict prevention later. |

## Sprint 7: Theatre/ICU

Full detail: `docs/sprints/sprint-7-theatre-icu.md`'s "Gap audit and Sprint 7.1 candidates" section,
written by a parallel session running concurrently with this one.

| Gap | Effort | Notes |
|---|---|---|
| `TheatreBooking.checklist`'s default content is 5 made-up items, not a real standard | Quick, seed-data change only | The real WHO Surgical Safety Checklist (19 items across Sign In/Time Out/Sign Out) is fully sourced verbatim in the sprint doc, ready to use as the new default — no schema change, `checklist` is already a free-form JSON map. |
| Surgical team is a single `surgeon_id`, no assistant surgeon/anaesthetist/scrub/circulating nurse | New small entity | Recommended: `theatre_staff_assignment` (`theatre_booking_id`, `staff_user_id`, `role` enum) rather than more columns on `TheatreBooking`, so team size/role mix scales without another migration. `surgeon_id` stays for backward compatibility. |
| Conflict detection only checks the theatre room, not staff (a surgeon or anaesthetist double-booked across two concurrent theatres) | Depends on the staff-assignment entity above | Real OR-scheduling systems check every named resource, not just the room. |
| No post-anaesthesia care unit (PACU/recovery) tracking between "completed" and discharge/ward-return | New small entity | Recommended: a new minimal `pacu_stay` entity mirroring `icu_episode`'s shape, not a reuse of `icu_episode` itself (most PACU patients are not critically ill) or a new `TheatreBooking` status (the booking tracks the room, PACU tracks the patient, concurrently not sequentially). |
| No structured operative/surgical report, only a status flag | New linked entity | `operative_note` (procedure performed, findings, complications, blood loss, implants, specimens, post-op diagnosis), a one-to-one linked entity authored after the procedure, JCAHO/AAAHC-standard components. |
| **Biomedical Equipment / Asset integration** — shipped this session | Done | Same integration as Sprint 6's row above, extended to `TheatreBooking.equipment_asset_ids`/`ICUEpisode.equipment_asset_ids`. |

## Referral / Transfer / Ambulance

Covered by a third parallel session running concurrently with this one. Landed in
`docs/architecture.md`'s "Referral, Transfer & Ambulance Billing" section, plus additive fields on
`docs/erd.md`'s `referral`/`ambulance_booking` rows and a new `patient_transfer` table (the latter
was pulled forward and actually shipped as part of Sprint 6 itself, since a transfer concept was a
genuine gap in an admit/discharge-only sprint — see `sprint-6-inpatient.md`). The inter-facility
`Referral` enrichment (receiving-facility fields, counter-referral) and the ambulance-fare billing
linkage remain proposed, not built — full detail in `docs/architecture.md`'s own section rather than
duplicated here.

## Suggested priority if picking this up

Rough ordering by patient-safety or revenue impact weighed against effort, not a strict ranking.

1. Critical-value lab alerting (Sprint 3). A patient-safety gap, and the underlying `flag` field
   already exists.
2. Refund/credit-note workflow (Sprint 5). The treasury-api primitive already exists and is unused,
   so this is close to a pure wiring task.
3. Allergy-recheck auto-trigger on `UpdatePatient` (Sprint 4). The recheck mechanism already exists
   and is correct; only one call site is missing.
4. OPD queue acuity-based reordering (Sprint 1). The acuity data already exists and is captured at
   triage; only the read-side ordering is missing.
5. Admission deposit collection (Sprint 5). Directly mirrors an existing, proven charge-posting
   pattern from Sprints 1/2, and closes a real revenue-collection gap for inpatient stays.

Everything else in this document is either a larger new module (MAR, real patient-merge, specimen
tracking) or a lower-urgency additive field (Maisha Number type, SHA beneficiary number, family
linkage, professional license numbers). All still worth doing, just not the first things to reach
for if time is short.
