# Hospital API — Sprint 9: Ambulance & Emergency Dispatch + Asset/Equipment Integration

**Status:** ⏳ Planned
**Depends on:** Sprint 5 (Billing, for ambulance-fee invoicing)
**Goal:** Thin reference integration into logistics-api for ambulance dispatch (no new fleet/dispatch engine), and a read-only surface over inventory-api's existing Asset/AssetMaintenance register as "Biomedical Equipment". Afya Hospital tier.

## Context

Both features in this sprint are **integration work, not new domain engines** — the whole point of
this sprint is to *not* build what already exists elsewhere on the platform. See
`docs/integrations.md` § 1.5 and § 2A for the full ADRs.

## Ent Schemas to Add

- `ambulance_booking` — `patient_visit_id` (nullable — a call may precede patient registration),
  `logistics_task_id`, `pickup_location`, `status`, `fare_amount`. **Reference row only.**
  **Added to the plan 2026-09-02** (see "Billing linkage" below): `patient_account_id` (nullable),
  `billable_charge_id` (nullable), `referral_id` (nullable), `patient_transfer_id` (nullable) —
  wires the fare into the Distributed Billing ledger when the booking belongs to a patient who already
  has, or is given, one, and links the booking back to whichever referral/transfer required it.
- `ambulance_membership` (optional, can slip to a later phase if time-constrained) — `tenant_id`,
  `crm_contact_id`, `plan_type` (individual/family), `expires_at`.
- **No new schema for assets** — this sprint is 100% integration against inventory-api's existing
  `Asset`/`AssetMaintenance`.

## Billing linkage (added to the plan 2026-09-02)

A gap audit found `fare_amount` had nowhere real to go once known — it was never actually posted as a
`BillableCharge`. Full design and sourcing (Kenyan ambulance pricing/product-line research, general
inter-facility-transport billing patterns): `docs/architecture.md`'s "Referral, Transfer & Ambulance
Billing" section and `docs/integrations.md` §2A.1.

- **Attached to an existing ledger**: if the booking belongs to a patient with an open
  `PatientAccount` (an admitted inpatient's inter-facility transfer via Sprint 6, or an OPD patient
  whose visit already opened an account), the fare posts as a normal `BillableCharge`
  (`department: "ambulance"`) onto that SAME account once the completed logistics-api task returns a
  fare — no separate mini-invoice.
- **Standalone**: a call-out with no prior registration (`patient_visit_id` null) leaves
  `patient_account_id`/`billable_charge_id` null. If the patient is later registered, the charge can be
  posted retroactively; if the call never becomes a hospital-api patient record at all, the fare is
  either a standalone treasury-api transaction or entirely outside this platform's billing.
- **Referral/transfer-triggered booking**: a booking created from an inter-facility referral
  (Sprint 2's richer referral shape, once built) or an inter-facility transfer (Sprint 6) sets
  `referral_id`/`patient_transfer_id` so the ambulance leg is traceable back to the clinical decision
  that required it, in either direction (booking → referral/transfer, and vice versa).
- **Not modeled**: a distinct "referral coordination"/"transfer administrative" fee separate from the
  transport fare — no confirmed standard Kenyan practice for one was found. A tenant that wants one
  uses an ordinary `BillableItemCatalog` row, not a schema addition.

## Endpoints

- `POST /{tenant}/hospital/ambulance-bookings` — creates a `Task` on logistics-api
  (`task_type: "ambulance_dispatch"`), stores the returned `logistics_task_id`. Accepts optional
  `referral_id`/`patient_transfer_id` (added 2026-09-02, see above) when the booking arises from one.
- `GET /{tenant}/hospital/ambulance-bookings/{id}` — status, proxying live status from logistics-api
  where useful, cached from `logistics.task.assigned`/`logistics.task.completed` events otherwise.
- `GET /{tenant}/hospital/assets` — proxies inventory-api's asset list (read-through, no local cache
  table beyond what `shared/cache` already provides generically).
- `GET /{tenant}/hospital/assets/{id}/maintenance` — proxies inventory-api's maintenance history.

## Integration Points

- logistics-api: `POST /v1/{tenant}/tasks` with `task_type: "ambulance_dispatch"` (additive string
  value, zero logistics-api schema change), subscribe to `logistics.task.assigned`/`.completed`.
- inventory-api: `GET /v1/{tenant}/inventory/assets*` (confirm exact paths against inventory-api's
  own docs when this sprint starts — the asset handlers exist in inventory-api's ent layer but the
  HTTP handler surface should be double-checked, not assumed).
- treasury-api: reached indirectly, via Sprint 5's existing `billing.CollectCharge` primitive, once
  the ambulance fare has posted as a `BillableCharge` on the patient's `PatientAccount` (see "Billing
  linkage" above) — hospital-api does not call treasury-api directly from this sprint's own code for a
  ledger-attached booking, it reuses the same collect-charge path every other department already uses.
  A standalone booking with no `PatientAccount` may still go straight to a treasury-api transaction,
  same as `WalkInSale`'s pattern. `ambulance_membership`, if it ships this sprint, is a recurring
  membership charge via treasury-api, unrelated to the per-booking fare.

## Definition of Done

- [ ] Ambulance booking creates a real logistics-api task and receives status updates.
- [ ] Ambulance fare (distance-based, via logistics-api's existing `PricingRule`) reaches a treasury
      invoice correctly, either via a posted `BillableCharge` + the existing collect-charge path
      (ledger-attached booking) or as a standalone treasury-api transaction (no `PatientAccount`).
- [ ] A completed booking attached to an open `PatientAccount` posts a real `BillableCharge`, and a
      standalone booking with no patient account correctly leaves the new linkage fields null instead
      of erroring (added 2026-09-02).
- [ ] Asset/equipment list and maintenance history render correctly from inventory-api's live data —
      confirmed zero local duplication (no `asset` table exists in hospital-api's schema).
- [ ] `go build`/`go vet` clean; no new logistics-api or inventory-api schema/migration was needed
      (confirms the reuse thesis from `docs/integrations.md`).

## Next Sprint

Sprint 10 — Specialized care programmes + KHIS/DHIS2 aggregate reporting.
