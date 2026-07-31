# Hospital API — Sprint 8: Blood Bank & Transfusion

**Status:** ⏳ Planned
**Depends on:** Sprint 4 (Pharmacy — reuses the same inventory-api lot/consumption integration pattern)
**Goal:** Donor registry, cross-match requests, transfusion records — Afya Hospital tier. Physical blood-unit stock is tracked in inventory-api, not a bespoke blood inventory system (see `docs/architecture.md` Data Authority table).

## Ent Schemas to Add

- `donor_record` — `tenant_id`, `full_name`, `blood_group`, `last_donation_at`, `eligibility_status`.
- `crossmatch_request` — `patient_visit_id`, `blood_group`, `units_requested`, `status`.
- `transfusion_record` — `crossmatch_request_id`, `inventory_lot_id`, `administered_at`, `administered_by`.

## Endpoints

- `POST /{tenant}/hospital/donors` — register a donor.
- `POST /{tenant}/hospital/crossmatch-requests` — request blood for a patient.
- `POST /{tenant}/hospital/crossmatch-requests/{id}/fulfill` — link to an inventory-api blood-unit lot, mark fulfilled.
- `POST /{tenant}/hospital/transfusions` — record a transfusion event.

## Integration Points

- inventory-api: blood units modeled as a `BLOOD` item category with `InventoryLot` batch/expiry
  tracking (same mechanism as drugs) — calls the same consumption/reservation endpoints pharmacy
  dispensing uses (see `docs/integrations.md` § 1.6). **No new inventory system.**
- Most private Kenyan facilities source blood from the National Blood Transfusion Service (NBTS)
  rather than running an independent blood bank — this module should support recording units
  received from NBTS as inbound stock (a purchase-order-like flow against inventory-api) as well as
  in-house donor collection.

## Definition of Done

- [ ] Cross-match request → fulfill from an inventory-api lot → transfusion record happy path works end to end.
- [ ] Expiry alerts for blood lots surface via the existing `inventory.lot.expiry_warning` subscriber (no new alert mechanism).
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.

## Next Sprint

Sprint 9 — Ambulance & Emergency Dispatch + Asset/Equipment integration.
