# Hospital API — Sprint 4: Pharmacy & Dispensing

**Status:** ⏳ Planned
**Depends on:** Sprint 2 (Examination, for prescribing) — can run in parallel with Sprint 3
**Goal:** Full prescription/dispensing workflow to feature parity with pos-api's existing pharmacy module, **plus** the standalone-chemist module-toggle configuration. This is Phase A + Phase B of `docs/migration-pos-pharmacy.md` — read that document in full before starting this sprint.

## Context

This is the highest-stakes sprint in the roadmap: it's the acceptance gate for the pos-api pharmacy
migration (Phase A) and it establishes hospital-api as the platform's **only** home for pharmacy
logic, for every facility size (Phase B). Reference pos-api's `pharmacy*.go`/`Prescription`/
`ControlledSubstanceLog`/`DrugInteractionCheck` for the feature list, but **rebuild the inventory-api
integration using `shared/service-client`** — pos-api's `internal/modules/inventory/client.go` is a
hand-rolled `net/http` client and is explicitly not the pattern to copy (see `docs/integrations.md`
§ 1 note).

## Ent Schemas to Add

- `prescription` — `patient_visit_id`, `prescribed_by`, `status` (draft/approved/locked/rejected/cancelled).
- `prescription_line` — `prescription_id`, `inventory_item_id`, `dose`, `duration`, `drug_name_snapshot`, `lot_id_snapshot`.
- `controlled_substance_log` — `prescription_line_id`, `witnessed_by`, `dispensed_by`, `lot_number`, `expiry_date` (dual-witness register).
- `drug_interaction_check` — `prescription_id`, `checked_at`, `findings` (JSON snapshot from inventory-api's interaction-check response).

## Endpoints

- `POST /{tenant}/hospital/prescriptions` — create from an examination.
- `POST /{tenant}/hospital/prescriptions/{id}/approve` / `/lock` / `/reject` / `/cancel` — lifecycle, matching pos-api's existing state machine.
- `POST /{tenant}/hospital/prescriptions/{id}/dispense` — dispense (calls inventory-api consumption via the fixed FEFO-aware `ConsumeReservation`, posts a `BillableCharge` per line — Sprint 5, priced from inventory-api's `ItemPricing` — triggers the controlled-substance dual-witness flow when applicable). Pharmacy defaults to `direct` collection mode at every facility tier including a standalone chemist (Phase B below) — the prescriber/pharmacist collects payment on the spot, same as today's pos-api "direct" workflow mode; `billing_queue` remains available as a tenant override for larger facilities that want a dedicated cashier.
- `GET /{tenant}/hospital/prescriptions/{id}/label.pdf` — dispensing label (adopt the `treasury-api/internal/modules/docs` fpdf engine per `docs/architecture.md`'s Runtime Document Generation note — do **not** copy pos-api's `report_pdf_pharmacy.go`/`printing/dispensing_label.go` verbatim, rebuild on the shared engine).

## Integration Points

- `inventory-api`: drug lookup, interaction check, lot consumption/reservation — see `docs/integrations.md` § 1.1-1.4, via `shared/service-client`.
- `treasury-api`: per-dispense billing line, collected via Sprint 5's `BillableCharge`/`collect` primitive (never a direct treasury call from this module) — see `docs/integrations.md` § 2.1 and `sprint-5-billing-insurance.md`. On a standalone-chemist tenant this IS the entire Billing module — no `PatientAccount`/ledger UI surfaced, just a walk-in-sale checkout, matching pos-api's pharmacy "direct" mode today.

## Phase B — Standalone-chemist configuration (same sprint)

- [ ] Add a tenant-level `enabled_modules` metadata field (JSON on the tenant/subscription config —
      no new schema table) so a tenant can run with **only** Pharmacy exposed.
- [ ] Register the standalone-chemist price point in subscriptions-api alongside the Afya tiers.
- [ ] Coordinate with subscriptions-api to migrate existing `POWERSUITE_DAWA_*` subscribers to the
      new configuration (a data task, not a schema change) — see `migration-pos-pharmacy.md` Phase B.3.

## Acceptance Gate (Definition of Done)

- [ ] Every workflow in `pos-service/pos-api/docs/sprints/sprint-8-pharmacy-module.md` and
      `sprint-9-service-module.md` is verified working in hospital-api, one by one.
- [ ] Controlled-substance dual-witness dispensing verified.
- [ ] Drug-interaction check verified against inventory-api's live `DrugInteractionRule` data.
- [ ] Dispensing label PDF renders via the shared fpdf engine.
- [ ] Standalone-chemist module toggle verified: a tenant with only Pharmacy enabled cannot reach
      Consultation/Lab/Inpatient routes (403, not just hidden in the UI).
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.
- [ ] **Only after all of the above**: proceed to `migration-pos-pharmacy.md` Phase C (per-tenant cutover) as a separate, later effort — not blocking this sprint's own completion.

## Next Sprint

Sprint 5 — Billing & Insurance.
