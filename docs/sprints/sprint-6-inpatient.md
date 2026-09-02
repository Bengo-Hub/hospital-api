# Hospital API — Sprint 6: Inpatient

**Status:** ✅ Shipped 2026-09-02 (`hospital-api@<pending-commit>` — see `.claude/plans/hospital-sprint6-inpatient-2026-09-02.md` for the full execution record). `go build`/`go vet`/`go test` green, Atlas migration generated + applied to the local dev DB, real integration test (`internal/integration/inpatient_golden_path_test.go`).
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

## Next Sprint

Sprint 7 — Theatre/OT scheduling + ICU/Critical-care monitoring.
