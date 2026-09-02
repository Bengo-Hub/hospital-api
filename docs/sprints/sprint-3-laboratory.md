# Hospital API — Sprint 3: Laboratory

**Status:** ✅ Shipped 2026-08-29 (`hospital-api@878e0ce`) — `LabTestCatalogDefault`/
`LabTestCatalogEntry` (a 12-test starter catalogue), `LabOrder`/`LabOrderLine`, real
`requires_prepayment` gating against Sprint 5-core's billing ledger (fails open when unconfigured),
and every endpoint below. Build/vet/test green; no live E2E walkthrough run yet (see the master
migration plan's Known Gaps).
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
- `GET /{tenant}/hospital/lab-test-catalog/entries`, `POST .../entries`, `PUT .../entries/{entryID}`,
  `POST .../entries/{entryID}/deactivate` — 2026-08-30, tenant Lab Test Catalog admin CRUD, gated
  `hospital.lab.view`/`hospital.lab.manage`. Was genuinely missing on both ends — a facility whose
  lab does anything beyond the ~20 globally-seeded starter tests had no way, UI or API, to add its
  own. Mirrors `BillableItemCatalog`'s CRUD shape. ✅ Built.
- `POST /{tenant}/hospital/lab-orders/{orderID}/insurance-claim` — see Sprint 5's doc for the full
  insurance-claim endpoint list (this one lives here because it's order-scoped). ✅ Built.

## Integration Points

- Publish `hospital.lab_order.resulted` on result entry → notifications-api sends the patient a
  "results ready" SMS/WhatsApp (per `docs/integrations.md` § 5).

## Definition of Done

- [x] Global lab-test catalogue seeded idempotently (12-test starter set via `internal/modules/refdata`).
- [ ] Order → collect → result → patient-notified happy path works end to end — **not yet run**
      against a live server/NATS this session; `hospital.lab_order.resulted` publishes on result
      entry, but notifications-api's actual consumption of it is unverified this round.
- [x] Atlas migration generated and committed.
- [x] `go build`/`go vet` clean.

## Next Sprint

Sprint 4 — Pharmacy & Dispensing (the first sprint that touches the pos-api migration — see
`docs/migration-pos-pharmacy.md` Phase A).

## Gap audit and MVP backlog candidates (2026-09-02)

Completeness audit of the shipped Laboratory module against real specimen-tracking and
critical-value-alerting practice in production lab information systems, done against the actual
shipped `internal/ent/schema/lab_order.go`/`lab_order_line.go` and `internal/modules/lab/
service.go`, not this sprint doc's own text. Both proposals below are **proposed, not yet built**.
Referred-out/external lab tracking is explicitly out of scope, covered elsewhere per this sprint
doc's own §2E note.

1. **Specimen / sample tracking.** `LabOrderLine` carries only `specimen_type` (a catalogue-snapshot
   string, e.g. "blood", "urine"). There is no collection timestamp, no collector identity, and no
   specimen ID/barcode field anywhere. Worth noting directly: `LabOrder.status` used to have a
   `collected` value, and it was deliberately removed on 2026-09-02 as confirmed dead code (the
   schema's own comment: "never set by any service method... zero live rows used it before removal").
   In other words, specimen collection has never really existed as a tracked event in this codebase,
   even nominally. The one enum value that gestured at it was removed specifically because nothing
   ever set it. Real lab practice treats collection (who drew it, when, against which specimen ID) as
   a distinct, safety-relevant step, since a mislabeled or late specimen is a genuine patient-safety
   failure mode, not just a workflow nicety. **Proposed**: additive nullable fields on
   `LabOrderLine`, `specimen_collected_at`, `specimen_collected_by`, `specimen_id` (a barcode/label
   string), plus a small `POST .../lines/{lineID}/collect` endpoint that sets them, gating result
   entry on collection having happened for that line. This effectively reintroduces a "collected"
   concept, but this time backed by real fields and an actual call site, not a dead enum value.
   Moderate effort: schema is small, but every worklist UI needs a "mark collected" step added before
   result entry.

2. **Critical-value / panic-value alerting.** `LabOrderLine.flag` (pending/normal/abnormal/critical)
   is set in `EnterResult`, but nothing distinguishes a critical result's downstream handling from a
   normal one. The only event published is `hospital.lab_order.resulted`, fired once EVERY line in
   the order has a result (not per-line, not per-critical-flag), and its payload carries only the
   `lab_order_id`, no severity information. A critical potassium result today reaches the ordering
   clinician through exactly the same "results ready" notification as a routine result, distinguished
   only by a red badge if and when someone opens the worklist. Fresh web research this session
   confirms critical-value reporting is a recognised patient-safety requirement (a Joint Commission
   National Patient Safety Goal in the US context, and the dominant real-world implementation is a
   direct, urgent notification to the ordering clinician, historically a phone call, increasingly SMS/
   push, always separate from routine result delivery). **Proposed**: when `EnterResult` sets
   `flag = critical`, publish a distinct `hospital.lab_order.critical_result` event carrying the
   ordering clinician's ID, test name, and value, for notifications-api to route as an urgent
   SMS/push distinct from the routine "results ready" patient message. No schema change (the `flag`
   field already exists and is already set correctly), a genuinely new code path in `EnterResult`
   plus a new notifications-api consumer. Small-to-moderate effort, high patient-safety value.

See `hospital-api/docs/mvp-gap-backlog-2026-09-02.md` for this item's place in the full
sprint-by-sprint backlog.
