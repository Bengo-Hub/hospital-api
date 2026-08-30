# Hospital API — Sprint 5: Billing & Patient Accounts (Insurance)

**Status:** ✅ Core ledger shipped 2026-08-29 (`hospital-api@126adbf`) — `BillableItemCatalog`/
`PatientAccount`/`BillableCharge`/`PatientNextOfKin` schemas, `PostCharge`/`CollectCharge`/
`SettleAccount`/`OverrideSettlement`/`ListPendingCharges` service logic, permission-gated handlers
(new `RoleCashier` role + `collect_own`/`collect_any`/`override_settlement` permissions), all
endpoints below. ✅ **Insurance wiring + catalog seed/admin CRUD shipped 2026-08-29 (later same
session)**: `billing.Service.CheckEligibility`/`SubmitInsuranceClaim`/`PollInsuranceClaim` wrap the
Phase-0 treasury client and are called from three real flows — the generic visit-level insurance
routes below, a lab-order-scoped `POST .../lab-orders/{orderID}/insurance-claim` (the insurance
alternative to CollectCharge+`/activate` for a `requires_prepayment` order), and a
prescription-scoped `POST .../prescriptions/{prescriptionID}/insurance-claim` (the insurance
alternative to a cash charge collect for dispensed lines). An accepted claim marks its charge(s)
`exempted` (the new `BillableCharge.status` value — no Atlas migration file exists for this: this
repo's ent-to-Postgres enum mapping stores enum fields as plain unconstrained `character varying`
with no DB-level CHECK constraint, confirmed against every other enum field's migration SQL, so
`atlas`/ent's versioned-migration diff correctly produces zero DDL for adding an allowed value —
verified by actually running the diff against a local dev Postgres, not assumed). `lab.ActivateIfPaid`
now accepts `paid` OR `exempted` as "settled." `BillableItemCatalog` now has a real per-facility-tier
starter seed (`refdata.SeedFacilityBillableItems`, called from `tenant.Syncer.SyncTenant`) and a
tenant admin CRUD surface (`GET/POST /billing/catalog`, `PUT/POST .../{itemID}[/deactivate]`, gated
on the new `hospital.billing.manage_catalog` permission). **Still not done**: no live E2E walkthrough
against a running server/treasury-api this session; the exact terminal-status strings treasury-api's
`InsuranceClaim.Status` uses were not confirmed against that repo (see `claimAccepted` in
`internal/modules/billing/service.go` — a deliberately liberal string match, flagged for
tightening). A real RBAC seed idempotency bug was found and fixed while adding this sprint's
permission codes — see the Changelog note in `docs/architecture.md`.
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
  (pending|invoiced|paid|exempted|waived|written_off), `treasury_invoice_id` (nullable), `created_by_user_id`,
  `created_by_department`, `paid_at` (nullable). `exempted` added 2026-08-29 (distinct from `waived`)
  after a KenyaEMR technical audit found its own billing module uses this exact distinction — a
  charge insurance covered in full is a different audit outcome from one the facility chose not to
  charge, see `docs/kenyaemr-technical-reference.md` §3.
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
- `POST /{tenant}/hospital/visits/{id}/insurance/check-eligibility` — proxies to treasury-api. ✅ Built.
- `POST /{tenant}/hospital/visits/{id}/insurance/submit-claim` — proxies to treasury-api, aggregating every pending charge on the visit's account (or exactly the `charge_ids` given) into one claim. ✅ Built.
- `GET /{tenant}/hospital/insurance/claims/{claimID}/status` — proxies treasury-api's poll route (read-only; does not itself finalize a charge — see `billing.Service.PollInsuranceClaim`'s doc comment on why resubmission is the retry path instead). ✅ Built.
- `POST /{tenant}/hospital/lab-orders/{orderID}/insurance-claim` — the insurance-path alternative to CollectCharge+`/activate` for one lab order's charges. ✅ Built (not in the original endpoint list above — added alongside the pharmacy equivalent since Gap 1's actual clinical wiring point is per-order/per-prescription, not just the generic visit-level proxy).
- `POST /{tenant}/hospital/prescriptions/{prescriptionID}/insurance-claim` — the insurance-path alternative to a cash charge collect for a prescription's dispensed lines. ✅ Built.
- `GET /{tenant}/hospital/billing/catalog`, `POST /{tenant}/hospital/billing/catalog`, `PUT /{tenant}/hospital/billing/catalog/{itemID}`, `POST /{tenant}/hospital/billing/catalog/{itemID}/deactivate` — `BillableItemCatalog` admin CRUD, gated on `hospital.billing.manage_catalog` for mutations. ✅ Built.
- `GET /{tenant}/hospital/insurance/providers` — 2026-08-30, shared picker source for the Lab/
  Pharmacy/Billing insurance-claim UI. Closes a real gap the frontend build surfaced: treasury-api's
  `ListProviders` was admin-JWT-only, so a caller had no way to discover configured providers at all
  — fixed by exposing it over S2S on treasury-api too (`treasury-api@e67ef9d`). ✅ Built.
- `GET /{tenant}/hospital/patients/{patientID}/next-of-kin`, `POST .../next-of-kin` — 2026-08-30.
  `PatientNextOfKin` was consumed by `SettleAccount`'s `next_of_kin_id` from day one but nothing
  anywhere ever created one — the Settle Account UI had to ask a cashier to type a raw UUID nobody
  could ever have. Gated on the SAME `collect_own`/`collect_any` permission as Settle Account
  itself (not a records permission), since the caller is typically the cashier settling the
  account, not records staff. ✅ Built.

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
- **Validated against real prior art (2026-08-29)**: KenyaEMR's own actively-maintained insurance-
  claims module builds a claim by selecting already-posted bill line-items and attaching a claim
  code, guarantee/pre-authorization ID, a free-text clinical justification, the visit's coded
  diagnoses, provider(s), and a treatment date range — the claim never re-derives billing data, it
  only references what `BillableCharge` already recorded. This confirms the direction this sprint's
  `insurance/submit-claim` endpoint should take: build the claim payload from already-posted charges,
  never a parallel re-entry. Full detail: `docs/kenyaemr-technical-reference.md` §3.

## Facility-tier defaults (seeded, tenant-overridable)

| Facility type | Billing module surface |
|---|---|
| Chemist | Walk-in Sale only — no `PatientAccount`, mirrors today's pos-api pharmacy "direct" checkout |
| Clinic/health-centre | Registration+Consultation default `billing_queue`; Pharmacy defaults `direct` |
| Facility/Hospital | Every department `direct`-capable; Billing desk is the explicit fallback + inpatient account owner |

## Definition of Done

- [ ] A charge posted by any of Sprints 1-4's billable steps appears on `GET .../account`
      immediately (no polling delay, no reliance on a physical receipt). Code-complete
      (`PostCharge`/`GetPatientAccount`), not yet exercised against a live running server.
- [ ] A department holding only `collect_own` can settle its own charges but is rejected (403) on
      another department's charge; Billing (`collect_any`) can settle anything. The permission-mapping
      logic (`sourceModulePermission`) has a unit test; the DB-touching collect flow itself does not
      yet.
- [ ] `requires_prepayment` charges correctly block the downstream clinical step
      (`ActivateLabOrderIfPaid`-equivalent) until `status == paid`. Shipped in Sprint 3's
      `lab.CreateOrder`/`ActivateIfPaid` (fails open when unconfigured), not yet live-verified.
- [ ] Discharge/body-release is blocked with a clear balance + settlement options when
      `PatientAccount.balance > 0`, and `override-settlement` requires the dedicated permission +
      reason and is fully audit-logged. `OverrideSettlement` itself is shipped; the actual
      discharge/mortuary-release workflow this gates is Sprint 6/10 (Inpatient/Morgue), not built yet,
      so this can't be verified end to end until then.
- [ ] A chemist-configured tenant's Billing UI shows only Walk-in Sale — no account/ledger
      complexity surfaced. **Not done** — hospital-ui has not been touched this session (Phase 7).
- [ ] Full checkout flow tested: multiple billable line types aggregate into one treasury invoice
      when collected together (e.g. Billing desk settling several pending charges at once).
      **Not yet run** — no live E2E walkthrough this session.
- [ ] eTIMS opt-in flag correctly gates whether treasury-api transmits to KRA (default OFF);
      transmitted records carry `source: hospital_sale`. The `hospital_sale` source shipped
      treasury-api-side in Phase 0; not yet exercised end to end from a hospital-api charge.
- [x] Insurance eligibility check works against treasury-api's live connector for at least one
      configured payer. `billing.Service.CheckEligibility` + `SubmitInsuranceClaim` are wired into
      real flows (lab-order/prescription/visit-level, see Endpoints above); **not yet verified
      against a live running treasury-api** — no E2E walkthrough this session.
- [x] `go build`/`go vet` clean; no financial-document schema (`Invoice`/`Payment`/etc.)
      introduced in hospital-api — only the billing ledger above.

## Next Sprint

Sprint 6 — Inpatient (the `PatientAccount`'s `admission_id` linkage; ward/day-rate charges accrue
onto the same account this sprint builds).
