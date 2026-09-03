# Hospital API — Sprint 6: Inpatient

**Status:** ✅ Shipped 2026-09-02 (`hospital-api@f0cdf9d` — see `.claude/plans/hospital-sprint6-inpatient-2026-09-02.md` for the full execution record). `go build`/`go vet`/`go test` green, Atlas migration generated + applied to the local dev DB, real integration test (`internal/integration/inpatient_golden_path_test.go`).
**Depends on:** Sprint 5 (Billing, for folio charges at discharge)
**Goal:** Ward/bed assignment, admission-to-discharge lifecycle, discharge summaries. First Afya Facility-tier-only feature (Afya Clinic tenants with the Inpatient add-on get a lightweight version — see below).

## Ent Schemas to Add

- `ward` — `tenant_id`, `outlet_id`, `name`, `capacity`, `billable_item_code` (nullable — added to the plan 2026-09-02, see below).
- `bed` — `ward_id`, `bed_number`, `status` (available/occupied/cleaning/out_of_service).
- `admission` — `patient_visit_id`, `bed_id`, `admitted_at`, `discharged_at`, `discharge_summary`. Creates a `PatientAccount` (Sprint 5) with `admission_id` set and `settlement_required_before: discharge` — ward/day-rate charges, and every other department's charges during the stay (lab, pharmacy, theatre), accrue onto this SAME account rather than each posting a separate mini-invoice.
- `patient_transfer` (added to the plan 2026-09-02, see "Ward/bed transfer" below) — `tenant_id`, `admission_id`, `transfer_type` (`intra_facility`|`inter_facility`), `from_ward_id`, `from_bed_id`, `to_ward_id` (nullable), `to_bed_id` (nullable), `receiving_facility_name` (nullable), `referral_id` (nullable), `ambulance_booking_id` (nullable), `reason`, `transferred_by`, `transferred_at`.

## Ward/bed transfer (added to the plan 2026-09-02)

A gap audit found this sprint's original scope had no transfer concept at all, only admit and
discharge. Full design reasoning and sourcing (OpenMRS's own ADT "Transfer" encounter type, Kenya's
national referral guideline's transfer requirements): `docs/architecture.md`'s "Referral, Transfer &
Ambulance Billing" section.

- **New endpoint**: `POST /{tenant}/hospital/admissions/{id}/transfer` — body carries `transfer_type`
  plus either `to_ward_id`/`to_bed_id` (intra-facility) or `receiving_facility_name` (+ optional
  `referral_id`/`ambulance_booking_id`) for inter-facility. Writes a `patient_transfer` row and updates
  `admission.bed_id` to the new bed for an intra-facility move; for an inter-facility move, it instead
  closes the admission (`discharged_at` set, discharge reason "transferred out") since the patient
  leaves this facility's care.
- **Billing decision**: an intra-facility ward transfer changes the running inpatient day-rate from the
  transfer date forward. `Ward.billable_item_code` names the `BillableItemCatalog` code that prices a
  day in that ward (e.g. `BED_DAY_GENERAL` vs `BED_DAY_ICU`); the daily bed-charge posting job resolves
  the code from whichever ward applies to that calendar day (via `patient_transfer` history for a past
  day, or the live bed/ward chain for today), so the rate changes automatically with no special-cased
  logic. An inter-facility transfer instead triggers ordinary discharge-time settlement on the account
  (same `PatientAccount.balance <= 0` gate the discharge endpoint below already enforces), since the
  admission is closing, not continuing at a new rate.
- **Ambulance leg**: an inter-facility transfer commonly needs an ambulance (see Sprint 9). This sprint
  does not build ambulance dispatch itself, it only stores the `ambulance_booking_id` reference on
  `patient_transfer` if one was arranged via Sprint 9's booking flow, exactly the same reference-only
  pattern the rest of this platform uses for logistics-api integration.

## Endpoints

- `POST /{tenant}/hospital/admissions` — admit a patient to a bed.
- `GET /{tenant}/hospital/wards/{id}/occupancy` — live bed-occupancy view.
- `POST /{tenant}/hospital/admissions/{id}/transfer` — ward/bed transfer or inter-facility transfer-out, see above (added 2026-09-02).
- `POST /{tenant}/hospital/admissions/{id}/discharge` — discharge + write summary. Blocked (409) while the linked `PatientAccount.balance > 0` — surfaces Record Payment / Apply Insurance / Write-Off / next-of-kin-settles options (Sprint 5's `settle`/`override-settlement` endpoints) rather than silently allowing discharge with an unpaid folio.
- `PATCH /{tenant}/hospital/beds/{id}/status` — housekeeping/bed-turnover status (available → cleaning → available), a lightweight status field, not a full housekeeping module.

## Afya Clinic "Inpatient add-on" scope note

The pricing model's Afya Clinic + Inpatient add-on (`CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS
PRICING MODEL.md`) is this same `ward`/`bed`/`admission` schema — the add-on is a subscription
feature-gate (`inpatient_module` in subscriptions-api), not a separate simplified schema. Small
clinics just use fewer wards/beds; the code path is identical.

## Integration Points

- Discharge triggers Sprint 5's checkout (folio → treasury invoice).
- Publish `hospital.visit.discharged`, and (added 2026-09-02) `hospital.admission.transferred`.
- An inter-facility transfer may reference an `ambulance_booking_id` from Sprint 9's dispatch flow (reference only, see above).

## Definition of Done

- [x] Admit → occupy bed → discharge → folio-checkout happy path works end to end — verified in
      `internal/integration/inpatient_golden_path_test.go`.
- [x] Admit → intra-facility transfer (ward/bed change) → discharge happy path works end to end.
      Day-rate segmentation: nights are allocated per ward from real `PatientTransfer` history
      (whole-elapsed-day floor per segment, any remainder attributed to the final segment) rather
      than the flat "current ward for the whole stay" the plan originally floated — see
      `inpatient.Service.postWardCharges`'s doc comment for the exact, deliberately-simplified
      allocation rule (true calendar-day/midnight-census attribution was judged not worth the
      added complexity for this pass). Not covered by an automated test with a real multi-day gap
      (the integration test's transfer happens same-instant, so it exercises the mechanism —
      ward/bed swap, `PatientTransfer` row — but not a >1-night split); worth adding if this
      billing path sees real multi-day-transfer usage.
- [x] Admit → inter-facility transfer-out (closes the admission, records `patient_transfer`) →
      account settles at transfer via the exact same `closeAdmission` gate `Discharge` uses (shares
      the code path, not a duplicate implementation).
- [ ] Bed-occupancy dashboard query performant at ward scale — functionally correct
      (`GetWardOccupancy` batches beds+admissions+patients in 3 queries regardless of ward size,
      no N+1), not yet load-tested against a large ward.
- [x] Atlas migration generated and applied to the local dev DB. `go build`/`go vet`/`go test`
      clean.

## Gap audit and Sprint 6.1 candidates (2026-09-02, later the same day)

**Shipped 2026-09-03**: every candidate below landed. `Ward.ward_type` (classification only,
`billable_item_code` still prices the ward) + `Bed.isolation_precaution` (set at `Admit`, cleared
at bed-turnover in `closeAdmission`, changeable mid-stay via a new `PATCH .../isolation-precaution`
route). Structured discharge summary fields on `Admission` (`discharge_diagnosis`/
`procedures_performed`/`discharge_medications`/`follow_up_instructions`/`condition_at_discharge`),
free-text `discharge_summary` kept as-is. New `VitalsChartEntry`/`WardRoundNote` entities +
`GET/POST /admissions/{admissionID}/{vitals-chart,ward-rounds}` (kept inside the existing
`inpatient` package/module rather than spinning off new ones — both are small, admission-scoped
records this package already owns the parent entity for). Transfer history: `PatientTransfer` rows
had literally zero HTTP-visible list route at all (not just a missing UI, as this section's own
text assumed) — added `ListTransfersByAdmission` + `GET /admissions/{admissionID}/transfers`
alongside the `TransferHistoryPanel`. Visitor log and the inventory-api `AssetReservation`
overlap-check gap remain explicitly not built, per this section's own recommendation.

**Found and fixed along the way**: the pre-existing `TestInpatientGoldenPath` integration test
asserted a zero admission-account balance immediately after `Admit` and one charge after a
department posted to it — both are now stale assumptions given Sprint 5's admission-deposit charge
(a `facility`-tier tenant now starts an admission with a 5000 balance from `ADMISSION_DEPOSIT`
alone); updated to assert 5000/5800/2 charges respectively. The main `TestGoldenPath` also needed a
`Collect` call inserted before its `EnterResult` call, since Sprint 3's specimen-collection gate
now hard-blocks result entry until a specimen is marked collected.

A client-facing engineer reviewed the shipped module and flagged that "IPD is also not complete, so
many sub modules still missing" — beds/assets weren't wired to inventory management, and the doc set
didn't reflect it. This section is a **research-grounded gap audit, proposed design only, nothing
below is built** — every field/entity here is additive to the shipped schema above, none of it
changes or removes a shipped field. Sourcing: KenyaEMR/OpenMRS's real Bed Management module
(`docs/kenyaemr-technical-reference.md`), OpenMRS's own `openmrs-module-bedmanagement` (confirmed via
its GitHub source — ships bed *types* and bed *tags* as first-class configuration, distinct from the
ward/bed identity fields themselves), the Joint Commission's discharge-summary content standard, and
CDC's transmission-based-precaution categories.

### Ward/bed types tied to billing rate

`Ward.billable_item_code` (shipped) already lets one ward price its own day-rate — the real gap is
that a ward has no *classification* of its own beyond its `name`, so there is nothing to drive a
picker in the UI ("this is a Private ward" vs "this is a General ward") or to default
`billable_item_code` sensibly when a ward is created. **Proposed**: a `ward.ward_type` enum
(`general`|`private`|`semi_private`|`isolation`|`icu`), nullable, additive. This does not replace
`billable_item_code` — a `private` ward still needs its own explicit code, since price varies by
facility — it only gives the UI something to group/filter wards by and a sensible default suggestion
when a new ward is created (e.g. suggest `BED_DAY_PRIVATE` for a `ward_type: private` ward, but let
the tenant override it, same as today).

**Isolation is deliberately NOT folded into `ward_type`.** A physical isolation ward is a real
category (OpenMRS's own bed-management module models bed *tags*, separate from bed type, for exactly
this kind of cross-cutting attribute), but infection-control isolation is also a per-PATIENT,
per-STAY state that can apply to a bed in an ordinary general ward (a patient on droplet precautions
in an otherwise general ward, pending an isolation bed becoming free) — it is not always tied to a
dedicated isolation ward existing at all. **Proposed**: `bed.isolation_precaution` (nullable enum:
`contact`|`droplet`|`airborne`|`none`, default `none`), set/cleared per admission, not a fixed
property of the ward. This maps directly to CDC's own transmission-based precaution categories
(Standard, Contact, Droplet, Airborne, and documented combinations of the three transmission-based
categories) — confirmed via CDC's own infection-control guidance, not invented categories. Clearing
it on discharge/bed-turnover is a workflow question for whoever implements this, not a schema
question.

### Structured discharge summary

`Admission.discharge_summary` (shipped) is a single free-text field. The Joint Commission's own
discharge-summary standard (confirmed via a direct read of its mandated-component definitions)
requires six elements: reason for hospitalization, significant findings (primary diagnoses),
procedures and treatment provided (hospital course/consults/procedures), the patient's condition at
discharge, patient/family instructions (discharge medications, activity/therapy/dietary instructions,
follow-up plans), and the attending physician's signature/attestation. **Proposed**: keep
`discharge_summary` exactly as-is (free-text narrative, backward compatible with anything already
written to it) and add structured, nullable, additive fields alongside it: `discharge_diagnosis`
(text, or a reference to `diagnosis_catalog_entry`/`_default` if the implementer wants it coded
rather than free text), `procedures_performed` (text), `discharge_medications` (text or JSON list),
`follow_up_instructions` (text), `condition_at_discharge` (enum:
`recovered`|`improved`|`unchanged`|`deteriorated`|`deceased`), and `discharged_by` already exists
(shipped) to cover the attending-physician-of-record element. The free-text `discharge_summary`
field remains available for anything that doesn't fit the structured fields — this is additive
richness, not a schema replacement.

### Nursing vitals during an inpatient stay — recommend a new entity, not `TriageRecord` reuse

**Question posed**: does inpatient vitals charting duplicate Sprint 2's `TriageRecord`, or does it
need its own entity? **Recommendation: a new entity.** `TriageRecord` is a single, one-shot
acuity-at-arrival row per `PatientVisit` (nurse-captured vitals + a `priority` field that only makes
sense once, at intake) — it was never designed to repeat. Inpatient vitals charting is structurally
different: a repeated time series (multiple readings per nursing shift, over a multi-day admission),
tied to the `admission_id` rather than the visit, with no meaningful "priority" concept at all.
Reusing `TriageRecord` would force many rows into a table whose own schema (a single `priority` enum,
no notion of "which reading in the series is this") was never built for repetition, and would blur an
OPD-intake workflow table with an IPD nursing-round workflow table across what should stay separate
RBAC/permission boundaries (`hospital.triage.*` vs an inpatient-nursing permission). **Proposed**: a
new `vitals_chart_entry` entity — `admission_id`, `recorded_by`, `recorded_at`, the same vitals shape
`TriageRecord` already uses for consistency (BP/temp/pulse/respiratory rate/SpO2), plus a `pain_score`
and free-text `notes`. Deliberately minimal, mirroring the existing "small, additive table per
workflow step" pattern this codebase already uses everywhere else, not a new subsystem.

### Doctor's ward rounds / progress notes — a related but distinct proposed entity

Closely related but a different author and cadence: a doctor's daily ward-round note is written once
or twice a day by a clinician, not a nurse, and often includes a running diagnosis/plan update, not
just vitals. **Proposed**: a second new entity, `ward_round_note` — `admission_id`, `clinician_id`,
`recorded_at`, `notes`, `diagnosis_id` (nullable, same catalog `ExaminationRecord` already
references) — conceptually `ExaminationRecord`'s shape, reapplied to an ongoing admission instead of
a single OPD consultation. Kept as its own entity rather than folded into `vitals_chart_entry`
because the two have different authors, different RBAC (`doctor` vs `nurse`), and different clinical
content (structured vitals vs free-text clinical reasoning).

### Next-of-kin / visitor logging

`PatientNextOfKin` (shipped, Sprint 5) already answers "who may settle this bill or authorize this
patient's discharge/release" — that is a legal/billing-authorization identity, and it is sufficient
for that purpose as-is, no gap found there. It is patient-level (not per-admission) and does not
model a physical visitor's check-in/check-out during a stay, which is a different concern (ward
access control / potential infection-control contact-tracing), not a billing one. **Proposed, lower
priority**: an optional `visitor_log` entity (`admission_id`, `visitor_name`, `relationship`,
`checked_in_at`, `checked_out_at`) if a facility actually wants physical visitor tracking — this is a
facilities/security feature more than a clinical one, and no research found it as a standard
"inpatient module" component the way bed types or discharge summaries are, so it's flagged as a real
but lower-confidence candidate: verify actual client demand before building it, rather than building
it speculatively.

### Asset/equipment wiring — see `docs/architecture.md` — shipped 2026-09-02

The "beds and other assets" integration the client flagged shipped the same day: `Bed` gained
`equipment_asset_ids` (a JSON array, not a single `asset_id` — `Ward` was NOT given this field,
equipment lives at the bed level) so a specific piece of biomedical equipment (a ventilator, a
monitor) can be linked to a physical bed, plus a read-only `/assets` "Biomedical Equipment" page
and an `EquipmentPickerModal` reused on the ward board's bed tiles in hospital-ui. Full design,
sourcing, and two concrete gaps found in inventory-api's own asset-reservation surface (and why
that surface was deliberately NOT used for this linkage): see `docs/architecture.md`'s "Biomedical
Equipment / Asset Integration" section and `erd.md`'s Bed row.

## Next Sprint

Sprint 7 — Theatre/OT scheduling + ICU/Critical-care monitoring.
