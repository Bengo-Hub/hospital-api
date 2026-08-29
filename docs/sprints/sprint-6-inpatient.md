# Hospital API — Sprint 6: Inpatient

**Status:** ⏳ Planned
**Depends on:** Sprint 5 (Billing, for folio charges at discharge)
**Goal:** Ward/bed assignment, admission-to-discharge lifecycle, discharge summaries. First Afya Facility-tier-only feature (Afya Clinic tenants with the Inpatient add-on get a lightweight version — see below).

## Ent Schemas to Add

- `ward` — `tenant_id`, `outlet_id`, `name`, `capacity`.
- `bed` — `ward_id`, `bed_number`, `status` (available/occupied/cleaning/out_of_service).
- `admission` — `patient_visit_id`, `bed_id`, `admitted_at`, `discharged_at`, `discharge_summary`. Creates a `PatientAccount` (Sprint 5) with `admission_id` set and `settlement_required_before: discharge` — ward/day-rate charges, and every other department's charges during the stay (lab, pharmacy, theatre), accrue onto this SAME account rather than each posting a separate mini-invoice.

## Endpoints

- `POST /{tenant}/hospital/admissions` — admit a patient to a bed.
- `GET /{tenant}/hospital/wards/{id}/occupancy` — live bed-occupancy view.
- `POST /{tenant}/hospital/admissions/{id}/discharge` — discharge + write summary. Blocked (409) while the linked `PatientAccount.balance > 0` — surfaces Record Payment / Apply Insurance / Write-Off / next-of-kin-settles options (Sprint 5's `settle`/`override-settlement` endpoints) rather than silently allowing discharge with an unpaid folio.
- `PATCH /{tenant}/hospital/beds/{id}/status` — housekeeping/bed-turnover status (available → cleaning → available), a lightweight status field, not a full housekeeping module.

## Afya Clinic "Inpatient add-on" scope note

The pricing model's Afya Clinic + Inpatient add-on (`CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS
PRICING MODEL.md`) is this same `ward`/`bed`/`admission` schema — the add-on is a subscription
feature-gate (`inpatient_module` in subscriptions-api), not a separate simplified schema. Small
clinics just use fewer wards/beds; the code path is identical.

## Integration Points

- Discharge triggers Sprint 5's checkout (folio → treasury invoice).
- Publish `hospital.visit.discharged`.

## Definition of Done

- [ ] Admit → occupy bed → discharge → folio-checkout happy path works end to end.
- [ ] Bed-occupancy dashboard query performant at ward scale.
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.

## Next Sprint

Sprint 7 — Theatre/OT scheduling + ICU/Critical-care monitoring.
