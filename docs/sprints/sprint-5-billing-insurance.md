# Hospital API — Sprint 5: Billing & Insurance

**Status:** ⏳ Planned
**Depends on:** Sprint 2/3/4 (billable events: consultation fee, lab fee, drug dispense charge)
**Goal:** Per-encounter charges aggregated into a treasury invoice; SHA/SHIF/NHIF eligibility and claims (opt-in per tenant/service); KRA eTIMS opt-in.

## Context

hospital-api owns **no** financial documents or insurance data (see `docs/architecture.md` Data
Authority table). This sprint is entirely about correct S2S orchestration against treasury-api's
existing, already-built DAWA insurance connector and invoicing engine — there is no new schema to
add on the money side.

## Ent Schemas to Add

- None for money itself. Add reference fields where charges originate: `invoice_id` (nullable) on
  `patient_visit`/`prescription`/`lab_order` as the created-invoice reference.
- `insurance_eligibility_check` (optional, audit-trail only) — `patient_visit_id`, `checked_at`,
  `provider`, `result` (JSON snapshot) — a thin log, not a duplicate of treasury-api's coverage data.

## Endpoints

- `POST /{tenant}/hospital/visits/{id}/checkout` — aggregates all billable lines for the visit
  (consultation fee, lab fee, drug dispense charges, theatre/ambulance fees once those sprints
  ship) and calls treasury-api to create the invoice.
- `POST /{tenant}/hospital/visits/{id}/insurance/check-eligibility` — proxies to treasury-api.
- `POST /{tenant}/hospital/visits/{id}/insurance/submit-claim` — proxies to treasury-api.

## Integration Points

- treasury-api invoice creation, insurance eligibility/claims, eTIMS opt-in flag per tenant/service
  — see `docs/integrations.md` § 2.1-2.3.
- Manual/CSV claim-upload fallback (per § 2.4) until treasury-api ships a Taifa Care HMIS adapter —
  build the UI/endpoint to not block on that external dependency.

## Definition of Done

- [ ] Full checkout flow tested: multiple billable line types aggregate into one treasury invoice.
- [ ] eTIMS opt-in flag correctly gates whether treasury-api transmits to KRA (default OFF).
- [ ] Insurance eligibility check works against treasury-api's live connector for at least one
      configured payer.
- [ ] `go build`/`go vet` clean; no new financial-document schema introduced in hospital-api.

## Next Sprint

Sprint 6 — Inpatient.
