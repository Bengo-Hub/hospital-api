# Hospital API — Sprint 3: Laboratory

**Status:** ⏳ Planned
**Depends on:** Sprint 2 (Consultation & Examination)
**Goal:** Test ordering, sample tracking, and result capture/delivery — in-house lab for Afya Facility/Hospital tiers; Afya Clinic tenants use this module only for referred-out result capture (no in-house test catalogue).

## Context

Same global-vs-tenant split as Sprint 2's diagnosis catalogue: the standard lab-test catalogue
(common panels — FBC, malaria smear, urinalysis, etc.) is global reference data; tenants may add
custom tests.

**Design note (2026-08-29):** this sprint's in-house result-entry workflow (order → collect →
result) covers on-site testing. Kenya's dominant clinical EMR models referred-out national testing
(viral load, early-infant-diagnosis, TB) as a separate batch courier-manifest workflow instead —
specimens grouped into a manifest with collection/dispatch dates and courier handoff details, sent
to a centralized reference lab, results returned asynchronously against the manifest, not as a live
device/HL7 integration. That pattern is out of scope for this sprint but worth keeping the
`lab_order`/`lab_order_line` shape compatible with, since it is a more realistic near-term
integration target than in-house analyzer connectivity for a Level 2-4 facility. Full detail:
`docs/integrations.md` §2E, `docs/kenyaemr-technical-reference.md` §8.

## Ent Schemas to Add

- `lab_test_catalog_default` — global (`code`, `name`, `specimen_type`, `reference_range`), no `tenant_id`.
- `lab_test_catalog_entry` — tenant-custom additions (nullable `tenant_id`).
- `lab_order` — `patient_visit_id`, `ordered_by`, `status` (requested/awaiting_payment/collected/resulted/cancelled). Ordering posts one `BillableCharge` per test (Sprint 5); if the catalog entry has `requires_prepayment: true` the order sits `awaiting_payment` until `GET .../account` shows it paid — mirrors pos-api's existing `ActivateLabOrderIfPaid` gate, generalized to any department's charge, not just lab.
- `lab_order_line` — `lab_order_id`, `lab_test_id`, `result_value`, `result_at`, `resulted_by`.

## Endpoints

- `POST /{tenant}/hospital/lab-orders` — create order from an examination/referral.
- `GET /{tenant}/hospital/lab-orders?status=` — lab worklist.
- `POST /{tenant}/hospital/lab-orders/{id}/lines/{lineId}/result` — enter a result.
- `GET /{tenant}/hospital/lab-test-catalog` — merged global + tenant-custom catalogue.

## Integration Points

- Publish `hospital.lab_order.resulted` on result entry → notifications-api sends the patient a
  "results ready" SMS/WhatsApp (per `docs/integrations.md` § 5).

## Definition of Done

- [ ] Global lab-test catalogue seeded idempotently.
- [ ] Order → collect → result → patient-notified happy path works end to end.
- [ ] Atlas migration generated and committed.
- [ ] `go build`/`go vet` clean.

## Next Sprint

Sprint 4 — Pharmacy & Dispensing (the first sprint that touches the pos-api migration — see
`docs/migration-pos-pharmacy.md` Phase A).
