# Hospital API — Sprint 5: Billing & Patient Accounts (Insurance)

**Status:** ⏳ Planned
**Depends on:** Sprint 1/2/3/4 (each posts a `BillableCharge` at its billable step)
**Goal:** A distributed billing ledger — every department can charge for what it does; the money
itself (invoices/payments/claims) always stays treasury-owned. See `docs/architecture.md`'s
"Distributed Billing & Patient Accounts" section for the full design rationale and external
research (KenyaEMR's "Billable Service Configurations", 2026 point-of-care collection research).

## Context

hospital-api owns **no** financial documents or insurance data (see `docs/architecture.md` Data
Authority table) — that discipline is unchanged. What this sprint adds is the **billing ledger**:
the record of what a patient owes, who charged it, whether it's settled, and who may collect it.
This directly answers the "patient keeps circling back to one cashier window" problem: a naive
design routes every charge through a single end-of-visit checkout; this design lets any
permission-holding department collect what it charged, with Billing as the fallback and the owner
of first-contact fees + inpatient accounts.

## Ent Schemas to Add

- `BillableItemCatalog` — tenant-configured price list, seeded with sane defaults per facility
  tier at provisioning time. Fields: `department` (records|triage|consultation|lab|pharmacy|
  theatre|inpatient|mortuary), `code`, `name`, `price` (nullable — null means "priced elsewhere",
  e.g. drugs price from inventory-api's `ItemPricing`, lab tests from `LabTest.price`), `applies_to`
  (first_visit|return_visit|all), `requires_prepayment` (bool), `collection_mode`
  (direct|billing_queue|either), `is_active`. Tenant-scoped (not global reference data — pricing is
  a real per-tenant business decision), but a facility-tier default seed set ships with each plan.
- `PatientAccount` — `patient_id`, `admission_id` (nullable — set for an inpatient stay, null for
  a standalone OPD visit account), `visit_id` (nullable, the OPD case), `status`
  (open|settled|written_off), `total_charged`, `total_paid`, `balance`, `settlement_required_before`
  (nothing|discharge|body_release), `next_of_kin_id` (nullable).
- `BillableCharge` — `patient_account_id`, `billable_item_id` (nullable — free-form charges
  allowed), `source_module`, `source_reference_id` (nullable UUID — the `LabOrder`/`Prescription`/
  `Admission`/etc that generated this line), `description`, `amount`, `status`
  (pending|invoiced|paid|waived|written_off), `treasury_invoice_id` (nullable), `created_by_user_id`,
  `created_by_department`, `paid_at` (nullable).
- `PatientNextOfKin` — `patient_id`, `name`, `phone`, `relationship`, `id_number`, `is_primary`.

## Endpoints

- `POST /{tenant}/hospital/billing/charges` — internal helper other modules call (not
  typically exposed to end users directly) to post a new `BillableCharge` against a patient's
  account. Sprints 1-4 call this at their respective billable step (registration fee, consultation
  fee, lab-test order, drug dispense) instead of building their own charge logic.
- `GET /{tenant}/hospital/patients/{id}/account` — the ledger any department reads to check
  paid/unpaid status ("the receipt every department can see" — see architecture.md).
- `POST /{tenant}/hospital/billing/charges/{id}/collect` — collects payment for ONE charge.
  Gated on `hospital.billing.collect_own` (only the charge's own `created_by_department`) or
  `hospital.billing.collect_any` (Billing desk). Creates a treasury invoice/payment intent via the
  existing S2S client (`POST /invoices` then a payment intent — reuses treasury-api's existing
  mixed-line-aggregation invoicing, no new treasury-api work), marks the charge paid, updates the
  account balance.
- `GET /{tenant}/hospital/billing/queue` — Billing desk's "Pending Charges" queue, every unpaid
  charge across every department/patient (mirrors pos-api's `ListBills` pattern). Gated on
  `hospital.billing.collect_any`.
- `POST /{tenant}/hospital/billing/accounts/{id}/settle` — settles the full outstanding balance
  in one call (discharge/body-release flow), optionally recording a `next_of_kin_id` as the payer.
- `POST /{tenant}/hospital/billing/accounts/{id}/override-settlement` — releases a patient/body
  with an outstanding balance. Gated on `hospital.billing.override_settlement`, requires a `reason`
  string, fully audit-logged.
- `POST /{tenant}/hospital/visits/{id}/insurance/check-eligibility` — proxies to treasury-api.
- `POST /{tenant}/hospital/visits/{id}/insurance/submit-claim` — proxies to treasury-api.
- `GET /{tenant}/hospital/insurance/claims/{claimID}/status` — proxies treasury-api's poll route.

## Integration Points

- treasury-api invoice/payment-intent creation, insurance eligibility/claims/coverage (all real,
  fully built — see `docs/integrations.md` §2.1-2.3), eTIMS opt-in flag per tenant/service, now
  attributed under the `hospital_sale` eTIMS source (added treasury-api-side, 2026-08-29) instead
  of being lumped under `pos_sale`.
- Manual/CSV claim-upload fallback (per §2.4) until treasury-api ships a Taifa Care HMIS adapter.
- The confirmed Taifa Care claim shape (FHIR `Bundle`, ICD-11 diagnosis codes, async
  submit-then-poll) lives in `docs/integrations.md` §2.4 and `docs/sha-taifacare-api-specs/`.
- DHA software certification (`docs/integrations.md` §2.5, `docs/compliance-kenya.md` §4) is a
  separate legal gate from this sprint's billing plumbing — tracked in Sprint 12, not here.

## Facility-tier defaults (seeded, tenant-overridable)

| Facility type | Billing module surface |
|---|---|
| Chemist | Walk-in Sale only — no `PatientAccount`, mirrors today's pos-api pharmacy "direct" checkout |
| Clinic/health-centre | Registration+Consultation default `billing_queue`; Pharmacy defaults `direct` |
| Facility/Hospital | Every department `direct`-capable; Billing desk is the explicit fallback + inpatient account owner |

## Definition of Done

- [ ] A charge posted by any of Sprints 1-4's billable steps appears on `GET .../account`
      immediately (no polling delay, no reliance on a physical receipt).
- [ ] A department holding only `collect_own` can settle its own charges but is rejected (403) on
      another department's charge; Billing (`collect_any`) can settle anything.
- [ ] `requires_prepayment` charges correctly block the downstream clinical step
      (`ActivateLabOrderIfPaid`-equivalent) until `status == paid`.
- [ ] Discharge/body-release is blocked with a clear balance + settlement options when
      `PatientAccount.balance > 0`, and `override-settlement` requires the dedicated permission +
      reason and is fully audit-logged.
- [ ] A chemist-configured tenant's Billing UI shows only Walk-in Sale — no account/ledger
      complexity surfaced.
- [ ] Full checkout flow tested: multiple billable line types aggregate into one treasury invoice
      when collected together (e.g. Billing desk settling several pending charges at once).
- [ ] eTIMS opt-in flag correctly gates whether treasury-api transmits to KRA (default OFF);
      transmitted records carry `source: hospital_sale`.
- [ ] Insurance eligibility check works against treasury-api's live connector for at least one
      configured payer.
- [ ] `go build`/`go vet` clean; no financial-document schema (`Invoice`/`Payment`/etc.)
      introduced in hospital-api — only the billing ledger above.

## Next Sprint

Sprint 6 — Inpatient (the `PatientAccount`'s `admission_id` linkage; ward/day-rate charges accrue
onto the same account this sprint builds).
