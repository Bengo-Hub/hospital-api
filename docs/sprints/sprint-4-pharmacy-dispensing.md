# Hospital API — Sprint 4: Pharmacy & Dispensing

**Status:** ✅ Core dispensing lifecycle shipped 2026-08-29 (`hospital-api@4005c21`) —
`Prescription`/`PrescriptionLine`/`ControlledSubstanceLog`/`DrugInteractionCheck` schemas, full
create→interaction-check→approve→lock→dispense→reject/cancel lifecycle, FEFO-aware dispense via
the fixed `ConsumeReservation`, controlled-substance dual-witness log, per-line `BillableCharge`
posting. Build/vet/test green. **Not yet shipped**: the `GET .../label.pdf` dispensing-label
endpoint below (no such route/handler exists yet) and the Phase B route-level `enabled_modules`
403 enforcement (that's Phase 6 of the master migration plan, still open) — see the Acceptance
Gate checklist for the exact state of each item.
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
- `GET /{tenant}/hospital/prescriptions/{id}/label.pdf` — dispensing label (adopt the `treasury-api/internal/modules/docs` fpdf engine per `docs/architecture.md`'s Runtime Document Generation note — do **not** copy pos-api's `report_pdf_pharmacy.go`/`printing/dispensing_label.go` verbatim, rebuild on the shared engine). **Not yet built** (2026-08-29) — `internal/http/handlers/pharmacy.go` has no label/PDF route yet; deferred, not silently dropped.

## Integration Points

- `inventory-api`: drug lookup, interaction check, lot consumption/reservation — see `docs/integrations.md` § 1.1-1.4, via `shared/service-client`.
- `treasury-api`: per-dispense billing line, collected via Sprint 5's `BillableCharge`/`collect` primitive (never a direct treasury call from this module) — see `docs/integrations.md` § 2.1 and `sprint-5-billing-insurance.md`. On a standalone-chemist tenant this IS the entire Billing module — no `PatientAccount`/ledger UI surfaced, just a walk-in-sale checkout, matching pos-api's pharmacy "direct" mode today.

## Phase B — Standalone-chemist configuration (same sprint)

- [x] Add a tenant-level `enabled_modules` metadata field (JSON on the tenant/subscription config —
      no new schema table) so a tenant can run with **only** Pharmacy exposed. Shipped as
      `Tenant.metadata JSON` in Phase 0 — **the field exists, but no route currently reads it to
      gate access** (see the Acceptance Gate item below, still open).
- [x] Register the standalone-chemist price point in subscriptions-api alongside the Afya tiers.
      Shipped 2026-08-29 as a genuine 4th, cheaper `AFYA_CHEMIST` tier (not a reuse of Afya Clinic's
      price point) in `subscriptions-api/cmd/seed/plans_hospital.go` — KES 4,500/month, features
      `pharmacy_dispensing`+`billing` only.
- [ ] Coordinate with subscriptions-api to migrate existing `POWERSUITE_DAWA_*` subscribers to the
      new configuration (a data task, not a schema change) — see `migration-pos-pharmacy.md` Phase B.3.
      **Not started** — this is a Phase 9-adjacent data-migration task, gated on pos-api's decisive
      removal, not this sprint's own scope.

## Acceptance Gate (Definition of Done)

- [ ] Every workflow in `pos-service/pos-api/docs/sprints/sprint-8-pharmacy-module.md` and
      `sprint-9-service-module.md` is verified working in hospital-api, one by one. **Not yet
      done** — no live E2E walkthrough has been run this session (Phase 8's job).
- [ ] Controlled-substance dual-witness dispensing verified. Code-complete (`Dispense` writes a
      `ControlledSubstanceLog` row for any line whose dual-witness was supplied) but not exercised
      against a running server yet.
- [ ] Drug-interaction check verified against inventory-api's live `DrugInteractionRule` data.
      Code-complete (`CreatePrescription` calls the real check-interactions endpoint) but not
      exercised live this session.
- [ ] Dispensing label PDF renders via the shared fpdf engine. **Not built** — no `label.pdf`
      route/handler exists yet, deferred rather than silently dropped.
- [ ] Standalone-chemist module toggle verified: a tenant with only Pharmacy enabled cannot reach
      Consultation/Lab/Inpatient routes (403, not just hidden in the UI). **Not built** — the
      `Tenant.metadata` field exists (Phase B above) but no router-level `enabled_modules` gate
      reads it yet; this is Phase 6 of the master migration plan, still open.
- [x] Atlas migration generated and committed. `go build`/`go vet` clean.
- [ ] **Only after all of the above**: proceed to `migration-pos-pharmacy.md` Phase C (per-tenant cutover) as a separate, later effort — not blocking this sprint's own completion.

## Next Sprint

Sprint 5 — Billing & Insurance.

## Gap audit and MVP backlog candidates (2026-09-02)

Completeness audit of the shipped Pharmacy/Dispensing module against real inpatient medication
practice and chronic-medication workflows, done against the actual shipped
`internal/ent/schema/prescription.go`/`prescription_line.go` and `internal/modules/pharmacy/
service.go`, not this sprint doc's own text. All proposals below are **proposed, not yet built**,
except item 3, which documents a real, already-shipped mechanism that just needs one more wiring
call.

1. **Medication Administration Record (MAR) for an inpatient.** Confirmed: `Prescription`/
   `PrescriptionLine` model exactly one event, a pharmacist DISPENSE (`quantity_dispensed`,
   `dispensed_by`, `dispensed_at`). There is no separate, per-dose record of a nurse actually
   administering a drug to an admitted patient. This matters more now than it would have before
   Sprint 6 shipped: an admitted patient on a multi-day IV antibiotic course has the WHOLE course
   dispensed to the ward in one pharmacy transaction, but nothing records whether each individual
   scheduled dose was actually given, by whom, at what time, or was missed/refused/held. Fresh web
   research confirms this is a standard, distinct HMIS concept: a Medication Administration Record is
   "the legal record of what was actually administered," explicitly separate from what pharmacy
   dispensed, precisely because a nurse charting each dose is a different act performed by a
   different role at a different time from the pharmacist's dispense. **Proposed**: a new, small
   `MedicationAdministration` entity (`prescription_line_id`, `admission_id`, `scheduled_time`,
   `administered_at` nullable, `administered_by` nullable, `status` enum
   given/refused/missed/held, `notes`), plus a nurse-facing "chart a dose" screen on the admission
   detail page. This is a genuinely new small module, not a field addition, and is the single item
   in this whole audit round most directly tied to something Sprint 6 just shipped (Admission) rather
   than to Sprint 4's own original scope, worth flagging for that reason.

2. **Prescription refill / repeat workflow for chronic medication.** `Prescription` has no concept of
   "this is a refill of prescription X." A chronic patient returning monthly for the same
   antihypertensive creates an entirely new, independently-typed `Prescription` every visit, with no
   linkage back to the original and no time saved for the prescriber. **Proposed**: an additive
   nullable `Prescription.repeat_of_prescription_id` (self-referencing) field, plus one new
   `CreateRefill(originalRxID)` service method that clones the original's lines into a new pending
   prescription for the prescriber to confirm rather than re-type. Additive, no destructive schema
   change, a genuinely useful quick win once the field and the one method exist.

3. **Allergy cross-check timing, already built, wiring gap only.** This one is good news: the
   mechanism this item asked about already exists. `pharmacy.Service.RecheckInteractions`
   (`internal/modules/pharmacy/service.go`) was added specifically for "a late-disclosed allergy...
   previously impossible: hospital-api only ever checked once, at creation" (2026-08-30). What is
   still missing: `patients.Service.UpdatePatient` (`internal/modules/patients/service.go`), which
   is what actually changes `Patient.allergy_flags`, has no hook that calls `RecheckInteractions` for
   that patient's still-open prescriptions. The recheck mechanism is real and correct; it just never
   fires automatically when the underlying allergy data changes upstream, only when someone manually
   triggers it on a specific prescription. **Proposed**: when `UpdatePatient` changes `Allergies` and
   the patient has any prescription in a pre-dispense state (pending/flagged/pharmacist_review/
   approved/locked), automatically call `RecheckInteractions` for each. Small, wiring-only, no new
   field or entity needed.

See `hospital-api/docs/mvp-gap-backlog-2026-09-02.md` for this item's place in the full
sprint-by-sprint backlog.
