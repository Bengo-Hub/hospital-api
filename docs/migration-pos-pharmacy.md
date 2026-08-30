# Migration Plan: pos-api Pharmacy/Clinical Logic → hospital-api

**Status:** Executed — complete as of 2026-08-29. hospital-api's Sprint 1-5 build brought the
pharmacy/clinical module to parity (see `docs/plan.md`'s Current State and `docs/sprints/`), and the
pos-api decisive-removal phase (§ below) landed the same day: pos-api carries zero pharmacy/OPD-clinical
code today, and the `DAWA` subscription family was retired from subscriptions-api. This document is
kept as the historical migration record.
**Last updated:** 2026-08-30 (status corrected — see `.claude/memory/project_pos_pharmacy_to_hospital_service_migration.md`)
**Owner decision:** hospital-api absorbs **all** pharmacy/dispensing logic, for **every** facility size. pos-api carries **no pharmacy logic at all** after this migration — not even for a standalone chemist. A chemist/dispensary is simply a hospital-api tenant with only the Pharmacy module enabled (below even the Afya Clinic tier). This corrects an earlier draft of this plan that proposed pos-api keep a standalone "Codevertex Dawa" chemist product — that proposal is **superseded**.

---

## 1. Why a clean cut, not a dual-write period

Per the platform's own decisive-removal convention (`feedback_erp_decisive_removal.md`, applied here by analogy from the ERP decomposition): when a domain moves to its rightful owning service, the old location is **fully deleted**, not kept alive behind a reference-ID shim or a feature flag. Two systems both claiming to own prescriptions/dispensing would immediately violate `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`'s "single source of truth" rule and reintroduce exactly the data-silo problem this whole initiative exists to fix in the wider Kenyan HMS market (see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md`'s research section).

**The only reason this migration is phased at all** is operational safety: pos-api's pharmacy module is live in production today (real tenants dispensing real drugs). The phasing exists to avoid a mid-migration window where a pharmacy tenant has neither a working pos-api pharmacy nor a working hospital-api pharmacy — not to preserve pos-api's pharmacy code long-term.

## 2. Current state (pos-api owns this today)

**Ent schemas** (`pos-service/pos-api/internal/ent/schema/`): `patient.go`, `patientvisit.go`, `triagerecord.go`, `examinationrecord.go`, `diagnosiscatalog.go`, `laborder.go`, `laborderline.go`, `labtest.go`, `prescription.go`, `prescriptionline.go`, `druginteractioncheck.go`, `controlledsubstancelog.go`.

**Handlers** (`pos-service/pos-api/internal/http/handlers/`): `pharmacy.go`, `pharmacy_checkout.go`, `pharmacy_controlled.go`, `clinical.go`, `clinical_records.go`, `clinical_triage.go`, `clinical_examination.go`, `clinical_lab.go`, `clinical_bills.go`, `clinical_catalog.go`, `clinical_settings.go`, `report_pdf_pharmacy.go`.

**Modules**: `internal/modules/printing/dispensing_label.go`, `internal/modules/inventory/client.go` (the drug-master/lot/interaction calls to inventory-api — hand-rolled HTTP client, **not** `shared/service-client`), `internal/modules/treasury/client.go` (insurance eligibility/claims calls to treasury-api, same hand-rolling issue), `cmd/seed/seed_clinical_catalogs.go`.

**Migrations**: `20260520213213_sprint8_9_pharmacy_service.sql`, `20260525014738_add_pharmacy_regulatory_fields.sql`, `20260721220942_prescription_metadata.sql`, `20260723141538_controlled_substance_log_lot_fields.sql`, `20260725054849_opd_clinical_workflow.sql`, `20260727230708_lab_test_catalog_diagnoses_workflow_mode.sql`.

**Frontend** (`pos-service/pos-ui/`): `app/[orgSlug]/pharmacy/**`, `app/[orgSlug]/patients`, `app/[orgSlug]/examination`, `components/clinical/**`, `components/pos/terminal/views/PharmacyTerminalView.tsx`, `components/settings/PharmacyWorkflowTab.tsx`, `hooks/usePharmacy.ts`, `hooks/useClinical.ts`, `lib/api/{pharmacy,clinical}.ts`.

**Docs**: `pos-service/pos-api/docs/sprints/sprint-8-pharmacy-module.md`, `sprint-9-service-module.md`.

**Subscriptions catalog**: per `usecase-powersuite-families-rollout.md` (project memory), pos-api's tiered feature families are `POWERSUITE_{HOSP|DUKA|DAWA}_{BASIC|PRO|GOLD}` — the `DAWA` family is the pharmacy/chemist tier line. This family must be retired from subscriptions-api and replaced by hospital-api's own `service_tag: "hospital"` tiers (Afya Clinic/Facility/Hospital, with Afya Clinic further configurable down to a pharmacy-only profile — see § 6).

## 3. Target state (hospital-api owns this after migration)

Same entity list, moved verbatim in meaning (names may be renamed/cleaned up, not required to match 1:1) into `hospital-api`'s `internal/ent/schema/`, per the ownership table already documented in `docs/architecture.md`. Integration calls to inventory-api (drug master/lot/interactions) and treasury-api (invoices/insurance) are **rebuilt using `shared/service-client`**, not copied from pos-api's hand-rolled client — this migration is also the opportunity to fix that known architectural debt (see `docs/plan.md` Technical Foundations).

## 4. Phased plan

### Phase A — Build hospital-api pharmacy module to parity (hospital-api Sprint 4, see `docs/sprints/sprint-4-pharmacy-dispensing.md`)
1. Author the ent schemas in hospital-api (`Prescription`, `PrescriptionLine`, `ControlledSubstanceLog`, `DrugInteractionCheck`) — informed by, not copy-pasted from, pos-api's versions (fix known issues along the way, e.g. adopt `shared/service-client`).
2. Build the inventory-api integration (drug lookup, interaction check, lot consumption/reservation) per `docs/integrations.md` § 1.
3. Build the treasury-api integration (per-dispense billing, insurance eligibility) per `docs/integrations.md` § 2.
4. Build hospital-ui's pharmacy/dispensing pages.
5. **Acceptance gate:** hospital-api's pharmacy module must support every real workflow pos-api's pharmacy module supports today (prescription creation/approval/lock/reject/cancel, checkout-to-order, controlled-substance dual-witness register, drug-interaction checks, dispensing labels) before Phase B starts. Verify by walking the exact feature list in `pos-service/pos-api/docs/sprints/sprint-8-pharmacy-module.md` and `sprint-9-service-module.md` line by line.

### Phase B — Configure hospital-api for the chemist/dispensary use case (hospital-api Sprint 4, cont.)
1. Add a tenant-level module toggle (`enabled_modules` on the tenant/subscription config — a JSON/metadata field, **not** a new schema table, per the "normalize via metadata additions" instruction) so a tenant can run hospital-api with **only** Pharmacy enabled (no Consultation/Lab/Inpatient UI or API surface exposed) — this is the direct replacement for pos-api's retired `DAWA` use-case family.
2. Register the standalone-chemist configuration as a fourth, smallest Codevertex Afya price point (below Afya Clinic) in the pricing doc and subscriptions-api catalog — reuse the existing tier machinery, don't build a parallel billing model.
3. Migrate `subscriptions-api`'s `POWERSUITE_DAWA_*` plans to the new hospital-api chemist configuration (existing DAWA subscribers get moved to the equivalent new plan — a subscriptions-api data task, not a schema change).

### Phase C — Tenant cutover (per tenant, not a big-bang)
1. For each tenant currently using pos-api pharmacy/DAWA: export their `Patient`/`Prescription`/`ControlledSubstanceLog` history from pos-api, import into hospital-api under the same `tenant_id` (a one-off data-migration script, not a permanent sync — matches the "decisive, not shimmed" principle).
2. Cut the tenant's pharmacy traffic over to hospital-api (DNS/routing/UI entry point change for that tenant).
3. Verify a full day of live dispensing on hospital-api before decommissioning that tenant's pos-api pharmacy access.

### Phase D — Decisive removal from pos-api (hospital-api "Launch" sprint, see `docs/sprints/sprint-13-launch-and-pos-decommission.md`)
Once **every** tenant using pos-api pharmacy has been cut over (Phase C repeated per tenant):
1. Delete all ent schemas, handlers, modules, and migrations listed in § 2 from `pos-service/pos-api`. Generate a new Atlas migration that **drops** the pharmacy/clinical tables (do not leave orphaned tables).
2. Delete the frontend routes/components/hooks listed in § 2 from `pos-service/pos-ui`.
3. Remove `pos-api`'s `use_case` enum values `pharmacy`/`dawa` entirely — valid `use_case` values become exactly: `retail`, `hospitality`, `quick_service`, `services`.
4. Update `pos-service/pos-api/docs/{plan,architecture,integrations,erd}.md` to remove every trace of pharmacy/clinical ownership (delete the sections, don't just mark them deprecated).
5. Remove the retired `POWERSUITE_DAWA_*` plan family from `subscriptions-api`'s seed once no tenant references it.
6. Update `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`'s hospital-api section to drop the "currently owned by pos-api pending migration" qualifier — hospital-api is simply the owner, full stop.
7. `go build ./...` + full test suite green on pos-api after removal (per `feedback_workflow_rules.md` § Post-Implementation Checklist) before pushing to main.

## 5. What does NOT move

pos-api's **retail, hospitality, quick_service, and services** use-cases are entirely unaffected — no schema, handler, or frontend change to those areas. The only pos-api entities in scope for removal are the clinical/pharmacy ones listed in § 2.

## 6. Standalone-chemist configuration recap (why this isn't a "hospital-api is overkill for a chemist" problem)

A chemist/dispensary is not a smaller *product* — it's the **Afya Clinic tier's Pharmacy module in isolation**, using the exact same schemas, drug-interaction checks, controlled-substance register, and inventory-api/treasury-api integrations that a full hospital uses, just with Consultation/Lab/Inpatient/etc. toggled off. This is strictly better than a parallel "Dawa" codebase: a chemist that later wants to add a consulting pharmacist or basic triage upgrades by toggling a module, not migrating to a different system.

## 7. Explicitly out of scope for this document

Actually executing Phases A-D is future work (hospital-api Sprints 4 and 13+, see `docs/sprints/`). This document is the finalized plan only, per the current round's scope.
