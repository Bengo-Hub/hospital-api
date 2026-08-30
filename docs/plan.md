# Hospital API — Plan

**Service:** hospital-api
**Product:** Codevertex Afya
**Language:** Go 1.26
**Production domain (planned):** `afyaapi.codevertexafrica.com`
**Last updated:** 2026-08-30
**Status:** Sprints 0-5 shipped (`go build`/`go vet`/`go test` green, Atlas migrations generated), live in production. Patient/OPD/Triage, Consultation/Examination, Laboratory, Pharmacy/Dispensing, and Billing/Insurance (ledger, collect/queue endpoints, insurance eligibility/claims, catalog CRUD) are real, working Go code — see "Current State" below and `docs/sprints/` for what shipped in each. hospital-ui has since been built to the same parity (Sprints 1-5 UI, RBAC-gated) — see `hospital-service/hospital-ui/docs/plan.md`. **2026-08-30:** the User Management Module (auth/roles/permissions) was fully rebuilt — see `docs/architecture.md`'s "User Management Module" section for the complete writeup; this is new capability beyond the original Sprint 0-13 roadmap below, not part of any numbered sprint.

---

## Product Overview

hospital-api is the Hospital Management Information System (HMIS) backbone for the Codevertex platform, sold as **Codevertex Afya**. It is a **standalone Go microservice** that gives a health facility one connected patient record across reception, consultation, laboratory, pharmacy, inpatient, and billing — instead of a paper file and disconnected registers.

**Entity ownership**: this service owns all clinical-workflow entities (Patient, PatientVisit, TriageRecord, ExaminationRecord, LabOrder, Prescription, ControlledSubstanceLog, Ward/Bed/Admission, specialized-care programme records). It does **NOT** own the drug/item master, lot/expiry tracking, or drug-interaction rules (owned by `inventory-api`), nor invoices/quotations/insurance-claims/payments (owned by `treasury-api`), nor tenant/user identity (owned by `auth-api`). See `docs/architecture.md` for the full data-authority table and **`shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`** for the canonical, platform-wide ownership matrix this service must respect.

**Why this exists now**: research into the Kenyan HMS market (see `d:\Projects\Codevertex\.claude\plans\hospital-service-codevertex-afya-2026-07-31.md` for the full writeup) found that most of the hard clinical/pharmacy logic already exists in `pos-service/pos-api` (built for pharmacy dispensing at a retail till) and needs to move into a dedicated hospital service that can also own OPD/triage/lab/inpatient workflows `pos-api` was never meant to carry. SHA's mandatory 2026 transition to Taifa Care HMIS (announced 2026-06-29, a roughly 90-day provider integration window with decontracting risk for facilities that miss it) makes a clean, dedicated hospital billing/claims integration a market necessity, not a nice-to-have.

A second research pass (2026-08-29, see `docs/compliance-kenya.md` and `docs/market-and-competitive-landscape.md`) sharpened this further: the Digital Health Act 2023 and its 2025 implementing regulations created a separate, legally binding requirement that the *software itself* be certified by the Digital Health Agency before a facility may use it against national health systems, on top of the SHA claims-integration deadline above. Competitively, the small-to-mid facility segment this client sits in (Level 2 to 4, ~20 patients/day) is underserved by the market's more visible players, several of which explicitly target Level 4 and above only. Both findings sharpen, rather than change, the existing roadmap below.

A third research pass, the same day, went deeper: a direct technical audit of KenyaEMR's real codebase (`docs/kenyaemr-technical-reference.md`) and a follow-up sweep of Kenya's national health-information-exchange architecture (`docs/compliance-kenya.md` §9-10). This confirmed the Distributed Billing design is a genuine advance over the market's dominant open-source system's real, cash-point-centric billing code, corrected the specialized-programmes list to match what Kenya's own clinical EMR actually tracks (VMMC, an OTZ adolescent-HIV cohort, PMTCT/EID follow-up, cervical and prostate cancer screening), surfaced a concrete 24-hour Shared Health Record update obligation and a national FHIR Implementation Guide programme worth designing toward, and added two previously-untracked competitor vendors (FunSoft, C-PAD) to the market picture. None of this changes the roadmap's shape, it sharpens the same modules with real, sourced detail.

---

## Current State (2026-08-29)

**Sprint-0 scaffold (2026-07-31)**:
- `internal/config` — env-var configuration (Postgres, Redis, NATS, Auth/JWKS, S2S service URLs).
- `internal/platform/{database,cache,events}` — pgx pool, Redis client, NATS connection + `hospital` JetStream stream (`hospital.>` subjects).
- `internal/http/{handlers,router}` — `/healthz`, `/readyz`, `/metrics`, and one JWKS-authenticated placeholder route (`GET /api/v1/{tenant}/hospital/ping`) proving the auth middleware chain works.
- `internal/app` — wires all of the above.
- `cmd/{api,migrate,seed}` — `api` runs the server. **`migrate`/`seed` were later found to be total no-op stubs** (see below) — fixed 2026-08-29 while adding Sprint 1's first real schema.

**Phase 0 groundwork (2026-08-29)**: `Tenant.metadata JSON` field (caches `facility_type`/`enabled_modules`); `internal/modules/{inventory,treasury}/client.go` on `shared-service-client`; `cmd/migrate/main.go` rebuilt from a no-op stub to the fleet-standard advisory-lock pattern; `scripts/entrypoint.sh` fixed to fail loudly on migrate errors instead of swallowing them.

**Sprints 1, 2, 5-core, 3, 4 (2026-08-29, same day, in that build order — see the master plan's build-order note)**: real Ent schemas, service-layer business logic, and RBAC-gated HTTP handlers now exist for Patient/OPD/Triage, Consultation/Examination/Diagnosis-Catalogue, the Billing ledger (`BillableItemCatalog`/`PatientAccount`/`BillableCharge`/`PatientNextOfKin`), Laboratory, and Pharmacy/Dispensing — the core pos-api migration target. `cmd/seed/main.go` was also found to be a no-op stub and fixed to actually run the new `internal/modules/refdata` global-catalogue seed. Full detail per sprint in `docs/sprints/sprint-{1,2,3,4,5}-*.md` and the progress log in `.claude/plans/pharmacy-to-hospital-service-migration-2026-08-29.md`.

**Since shipped (2026-08-29/30, not reflected in the section above at the time it was written)**: Sprint 5's insurance eligibility/claim wiring into the real checkout flow, `BillableItemCatalog` admin CRUD, hospital-ui built to full Sprint 1-5 parity (RBAC-gated sidebar, patient/billing/lab/pharmacy pages, Users/Config admin), and the pos-api decisive-removal phase (pos-api carries zero pharmacy/OPD-clinical code, `DAWA` subscription family retired) — see `.claude/plans/hospital-ui-rbac-and-feature-wiring-2026-08-30.md` and `project_pos_pharmacy_to_hospital_service_migration.md` in project memory.

**Not yet implemented**: Sprints 6-13 (Inpatient onward). Per-user outlet/branch membership enforcement remains a known gap (`internal/http/middleware/outlet_context.go` — any authenticated tenant user can select any outlet via `X-Outlet-ID` today). No live E2E request has been fired against a running server this session. See the phased roadmap below and `docs/sprints/`.

---

## Phased Roadmap

**Last revised:** 2026-07-31 (Round 2 — expanded to cover the full hospital department catalog after
market research; see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md` § Round 2).

| Sprint | Capability | Status |
|---|---|---|
| 0 | Foundations: repo scaffold, config/logging/db/redis/nats wiring, health checks, JWKS auth middleware | ✅ Scaffold shipped |
| 1 | Patient registry, OPD reception/queuing, Triage — migrated from pos-api's `Patient`/`PatientVisit`/`TriageRecord` | ✅ Shipped 2026-08-29 (`hospital-api@05741fd`) |
| 2 | Consultation & Examination — `ExaminationRecord`, `DiagnosisCatalog` (global reference + tenant custom) | ✅ Shipped 2026-08-29 (`hospital-api@709b140`) |
| 3 | Laboratory — `LabOrder`/`LabOrderLine`, `LabTest` catalogue (global reference data) | ✅ Shipped 2026-08-29 (`hospital-api@878e0ce`) |
| 4 | Pharmacy & Dispensing — migrated from pos-api's `Prescription`/`PrescriptionLine`/`ControlledSubstanceLog`; calls `inventory-api` for drug master/lot/interactions; includes the standalone-chemist module-toggle configuration (see `docs/migration-pos-pharmacy.md` § 6) | ✅ Core dispensing lifecycle shipped 2026-08-29 (`hospital-api@4005c21`); dispensing-label PDF endpoint and the route-level `enabled_modules` 403 gate are NOT yet built — see Sprint 4's doc |
| 5 | Billing & Insurance — calls `treasury-api` for invoices + SHA/SHIF/NHIF eligibility/claims (eTIMS opt-in per tenant/service) | ✅ Core ledger shipped 2026-08-29 (`hospital-api@126adbf`) — charge/collect/queue/settle/override-settlement; insurance eligibility/claim endpoints not yet wired into a checkout flow |
| 6 | Inpatient — Ward/Bed/Admission, discharge summaries | ⏳ Planned |
| 7 | Theatre/OT scheduling + ICU/Critical-care monitoring | ⏳ Planned |
| 8 | Blood Bank & Transfusion — donor registry, cross-match, transfusion records (built on inventory-api's lot tracking for physical blood units) | ⏳ Planned |
| 9 | Ambulance & Emergency Dispatch (thin reference into logistics-api's existing Task/FleetMember/PricingRule — no new dispatch engine) + Asset/Equipment integration (reference inventory-api's existing Asset/AssetMaintenance) | ⏳ Planned |
| 10 | Specialized care programmes — ANC, PNC, ART, TB, Immunization, Morgue (HosiPoa-parity features) + KHIS/DHIS2 aggregate reporting (ADX standard) | ⏳ Planned |
| 11 | Subscriptions/licensing (`service_tag: hospital`) + reporting/analytics dashboards | ⏳ Planned |
| 12 | Compliance hardening — Kenya DPA consent capture, audit trail, 20-year retention policy, Certificate of Data Handler alignment | ⏳ Planned |
| 13 | Launch — production readiness, runbooks, **decisive pos-api pharmacy-code decommission** (see `docs/migration-pos-pharmacy.md` Phase D) | ⏳ Planned |

See `docs/sprints/` for the detailed breakdown of every sprint (one file per sprint, `sprint-N-topic.md`).

---

## Technical Foundations

- **Language & Runtime:** Go 1.26, `gofmt`, `go vet`.
- **HTTP:** `chi v5` router + `go-chi/cors`, matching every sibling service (auth-api, inventory-api, library-api).
- **ORM:** `ent v0.14` (schema-as-code) + **Atlas versioned migrations** once schemas exist — never `ent` auto-migrate in production.
- **Data stores:** PostgreSQL via `pgx`; Redis for tenant-branding cache + ephemeral state.
- **Eventing:** NATS JetStream + `shared-events` (`github.com/Bengo-Hub/shared-events`) transactional outbox. Aggregate type is always `hospital`; subjects are `hospital.{resource}.{action}` (e.g. `hospital.patient.created`).
- **Shared libraries** (tagged GitHub releases only, never a local `replace ../x` path): `httpware` (middleware/tenant/outlet context), `shared-auth-client` (JWKS validation, via the `replace` directive `shared-auth-client => auth-client` since the module path and repo name differ), and — from Sprint 1 onward — `shared-service-client` (circuit-breaker S2S calls to inventory-api/treasury-api instead of a hand-rolled client, unlike pos-api's mistake) and `shared/cache`/`shared/pagination`.
- **Deployment:** Docker multi-stage build (three binaries: `hospital`, `hospital-migrate`, `hospital-seed`), Kubernetes via the centralized `devops-k8s` repo, ArgoCD GitOps, `minReplicas: 2` + PodDisruptionBudget from day one (platform HA baseline).
- **Observability:** zap structured logging, Prometheus `/metrics`, `/healthz` + `/readyz`.
- **Auth:** SSO via `auth-api` (JWKS RS256), Trinity Authorization (RBAC + Licensing + Resources) — see `docs/architecture.md`.

---

## Core Capabilities & Domain Modules (planned)

1. **Reception & OPD queue** — patient registration/check-in, appointment booking, single EMR shared by every module.
2. **Triage** — vitals (BP, temperature, pulse, respiration, weight, SpO2) and acuity/priority capture before the patient reaches the doctor queue, recorded by whichever clinical staff a facility assigns to it (a dedicated triage nurse at larger facilities, the same clinician at a small team) — see `docs/erd.md`'s `triage_record` and `docs/sprints/sprint-1-patient-opd-triage.md`. Not a role restriction, `recorded_by` is any authorised clinical user.
3. **Consultation** — doctor/dental/MCH/specialist queues, structured examination notes, diagnosis capture, referral to lab/pharmacy.
4. **Laboratory** — test requests, sample tracking, result capture and delivery back to the requesting clinician.
5. **Pharmacy & Dispensing** — prescription dispensing, OTC sale, drug-interaction/allergy checks (via inventory-api), controlled-substance dual-witness register. Module-togglable down to a standalone chemist/dispensary configuration (see `docs/migration-pos-pharmacy.md` § 6) — **this is now the only place pharmacy logic lives on the whole platform**; pos-api never carries it.
6. **Inpatient** — ward/bed assignment, admission-to-discharge, discharge summaries.
7. **Theatre / Operating Room** — surgery scheduling, OT checklist, staff/case-load assignment (booking-pattern mirrors pos-api's existing Facility/FacilityBooking resource-booking shape, but is clinically owned here).
8. **ICU / Critical Care** — bed-level vitals/monitoring flags, staff assignment, escalation alerts.
9. **Blood Bank & Transfusion** — donor registry, cross-match requests, transfusion records; physical blood-unit stock is tracked as a lot-tracked, short-shelf-life item in inventory-api (not a bespoke blood inventory system).
10. **Ambulance & Emergency Dispatch** — thin reference into logistics-api's existing Task (`task_type: ambulance_dispatch`, an additive string value, no schema change)/FleetMember (tagged `ambulance`)/PricingRule (`distance` rule type, matching Kenya's base-fee-plus-per-km ambulance pricing model) — hospital-api does not build a second dispatch/fleet engine. Optional recurring "ambulance membership" product (mirrors St John Kenya's individual/family annual plan) billed via treasury-api.
11. **Asset / Equipment integration** — surfaces inventory-api's existing `Asset`/`AssetMaintenance` register (already covers biomedical equipment, beds, ambulances-as-capital-assets, warranty, maintenance schedules) as "Biomedical Equipment" in the hospital-api UI; hospital-api references `asset_id`, never owns a parallel asset register. Depreciation accounting is already wired via treasury-api's `FixedAssetDepreciation`.
12. **Billing & Insurance** — per-encounter charges aggregated into a treasury invoice; SHA/SHIF/NHIF eligibility verification and claims submission via treasury-api's existing insurance connector; KRA eTIMS transmission (treasury-owned) is an **opt-in per tenant/service**, not applied to every encounter by default — many clinical services are not required to carry a fiscal invoice.
13. **Specialized care programmes** — ANC, PNC, ART (with an Operation Triple Zero adolescent-adherence cohort flag), TB, Immunization, VMMC (Voluntary Medical Male Circumcision), HIV-Exposed Infant/PMTCT follow-up, cervical and prostate cancer screening (MOH-reporting aligned), Morgue management. Expanded 2026-08-29 after a KenyaEMR technical audit found these are the real programmes Kenya's dominant clinical EMR tracks as distinct modules, not an internally-invented list — see `docs/kenyaemr-technical-reference.md` §4.
14. **KHIS/DHIS2 aggregate reporting** — indicator export via the ADX standard for public/donor-funded programmes (ART, TB, immunization) that must report into Kenya's national KHIS, distinct from SHA/Taifa Care insurance-claims reporting.
15. **CSSD (sterilization) & Dietary** — lightweight tracking modules for sub-departments that support inpatient care.
16. **Patient communications** — SMS/WhatsApp appointment reminders, lab-result-ready, prescription-ready alerts via notifications-api.
17. **Reporting** — occupancy, revenue, and clinical-throughput dashboards (delegating financial aggregation to treasury-api, never re-summing). Design cue from DHA's own RMNCAH platform tender (`docs/compliance-kenya.md` §8): national/regulator-facing dashboards over health event data are expected to support facility/county/national drill-down, disaggregation by facility and indicator, and role-based views (clinician vs. facility admin vs. programme/donor reporting user) rather than a single flat view. Worth building this capability with that shape in mind even before any specific national integration exists.
18. **Compliance & audit** — Kenya Data Protection Act-aligned consent capture, audit trail, and retention policy for sensitive health data.

**Explicitly out of scope for Codevertex Afya v1** (deferred, not silently dropped): full PACS/RIS radiology image storage (an integration point to a third-party PACS via DICOM, not stored in hospital-api itself), payroll/HR duty rostering (already owned by `erp-api` — hospital-api references staff via `auth_service_user_id` like every other service), and full facilities/security/visitor-management (out of scope for a health-records system).

---

## References

- [Architecture](architecture.md)
- [Integrations](integrations.md)
- [Entity Relationship Diagram](erd.md)
- [Compliance & Certification Reference (Kenya)](compliance-kenya.md)
- [Market & Competitive Landscape (Kenya)](market-and-competitive-landscape.md)
- [KenyaEMR Technical Architecture Reference](kenyaemr-technical-reference.md)
- [SHA/Taifa Care official API specifications](sha-taifacare-api-specs/)
- [pos-api Pharmacy Migration Plan](migration-pos-pharmacy.md)
- [Sprint Plans](sprints/)
- `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md` — canonical cross-service data-ownership matrix
- `shared-docs/TRINITY-AUTHORIZATION-PATTERN.md` — RBAC + Licensing + Resources authorization model
- `shared-docs/event-architecture.md` — uniform event-subject convention and service event catalog
- `d:\Projects\Codevertex\CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS PRICING MODEL.md` — Codevertex Afya tiered pricing
