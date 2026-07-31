# Hospital API — Plan

**Service:** hospital-api
**Product:** Codevertex Afya
**Language:** Go 1.26
**Production domain (planned):** `hospitalapi.codevertexafrica.com`
**Last updated:** 2026-07-31
**Status:** Sprint-0 scaffold shipped (config/logging/db-pool/redis/nats/health/JWKS-auth wired, `go build`/`go vet` clean, health/readiness/protected-route smoke-tested). No domain ent schemas or business logic yet.

---

## Product Overview

hospital-api is the Hospital Management Information System (HMIS) backbone for the Codevertex platform, sold as **Codevertex Afya**. It is a **standalone Go microservice** that gives a health facility one connected patient record across reception, consultation, laboratory, pharmacy, inpatient, and billing — instead of a paper file and disconnected registers.

**Entity ownership**: this service owns all clinical-workflow entities (Patient, PatientVisit, TriageRecord, ExaminationRecord, LabOrder, Prescription, ControlledSubstanceLog, Ward/Bed/Admission, specialized-care programme records). It does **NOT** own the drug/item master, lot/expiry tracking, or drug-interaction rules (owned by `inventory-api`), nor invoices/quotations/insurance-claims/payments (owned by `treasury-api`), nor tenant/user identity (owned by `auth-api`). See `docs/architecture.md` for the full data-authority table and **`shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`** for the canonical, platform-wide ownership matrix this service must respect.

**Why this exists now**: research into the Kenyan HMS market (see `d:\Projects\Codevertex\.claude\plans\hospital-service-codevertex-afya-2026-07-31.md` for the full writeup) found that most of the hard clinical/pharmacy logic already exists in `pos-service/pos-api` (built for pharmacy dispensing at a retail till) and needs to move into a dedicated hospital service that can also own OPD/triage/lab/inpatient workflows `pos-api` was never meant to carry. SHA's mandatory 2026 transition to Taifa Care HMIS (90-day integration deadline, decontracting risk) makes a clean, dedicated hospital billing/claims integration a market necessity, not a nice-to-have.

---

## Current State (2026-07-31)

**Sprint-0 scaffold only.** Implemented:
- `internal/config` — env-var configuration (Postgres, Redis, NATS, Auth/JWKS, S2S service URLs).
- `internal/platform/{database,cache,events}` — pgx pool, Redis client, NATS connection + `hospital` JetStream stream (`hospital.>` subjects).
- `internal/http/{handlers,router}` — `/healthz`, `/readyz`, `/metrics`, and one JWKS-authenticated placeholder route (`GET /api/v1/{tenant}/hospital/ping`) proving the auth middleware chain works.
- `internal/app` — wires all of the above; no ent client, no domain schemas, no outbox publisher yet (those need at least one ent schema to exist — see `internal/ent/schema/README.md`).
- `cmd/{api,migrate,seed}` — `api` runs the server; `migrate`/`seed` are no-op placeholders until Sprint 0 lands real schemas.

**Not yet implemented**: everything domain-specific (Patient, Visit, Consultation, Lab, Pharmacy, Inpatient, Billing integration, RBAC, events). See the phased roadmap below and `docs/sprints/`.

---

## Phased Roadmap

**Last revised:** 2026-07-31 (Round 2 — expanded to cover the full hospital department catalog after
market research; see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md` § Round 2).

| Sprint | Capability | Status |
|---|---|---|
| 0 | Foundations: repo scaffold, config/logging/db/redis/nats wiring, health checks, JWKS auth middleware | ✅ Scaffold shipped |
| 1 | Patient registry, OPD reception/queuing, Triage — migrated from pos-api's `Patient`/`PatientVisit`/`TriageRecord` | ⏳ Planned |
| 2 | Consultation & Examination — `ExaminationRecord`, `DiagnosisCatalog` (global reference + tenant custom) | ⏳ Planned |
| 3 | Laboratory — `LabOrder`/`LabOrderLine`, `LabTest` catalogue (global reference data) | ⏳ Planned |
| 4 | Pharmacy & Dispensing — migrated from pos-api's `Prescription`/`PrescriptionLine`/`ControlledSubstanceLog`; calls `inventory-api` for drug master/lot/interactions; includes the standalone-chemist module-toggle configuration (see `docs/migration-pos-pharmacy.md` § 6) | ⏳ Planned |
| 5 | Billing & Insurance — calls `treasury-api` for invoices + SHA/SHIF/NHIF eligibility/claims (eTIMS opt-in per tenant/service) | ⏳ Planned |
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
2. **Consultation** — doctor/dental/MCH/specialist queues, structured examination notes, diagnosis capture, referral to lab/pharmacy.
3. **Laboratory** — test requests, sample tracking, result capture and delivery back to the requesting clinician.
4. **Pharmacy & Dispensing** — prescription dispensing, OTC sale, drug-interaction/allergy checks (via inventory-api), controlled-substance dual-witness register. Module-togglable down to a standalone chemist/dispensary configuration (see `docs/migration-pos-pharmacy.md` § 6) — **this is now the only place pharmacy logic lives on the whole platform**; pos-api never carries it.
5. **Inpatient** — ward/bed assignment, admission-to-discharge, discharge summaries.
6. **Theatre / Operating Room** — surgery scheduling, OT checklist, staff/case-load assignment (booking-pattern mirrors pos-api's existing Facility/FacilityBooking resource-booking shape, but is clinically owned here).
7. **ICU / Critical Care** — bed-level vitals/monitoring flags, staff assignment, escalation alerts.
8. **Blood Bank & Transfusion** — donor registry, cross-match requests, transfusion records; physical blood-unit stock is tracked as a lot-tracked, short-shelf-life item in inventory-api (not a bespoke blood inventory system).
9. **Ambulance & Emergency Dispatch** — thin reference into logistics-api's existing Task (`task_type: ambulance_dispatch`, an additive string value, no schema change)/FleetMember (tagged `ambulance`)/PricingRule (`distance` rule type, matching Kenya's base-fee-plus-per-km ambulance pricing model) — hospital-api does not build a second dispatch/fleet engine. Optional recurring "ambulance membership" product (mirrors St John Kenya's individual/family annual plan) billed via treasury-api.
10. **Asset / Equipment integration** — surfaces inventory-api's existing `Asset`/`AssetMaintenance` register (already covers biomedical equipment, beds, ambulances-as-capital-assets, warranty, maintenance schedules) as "Biomedical Equipment" in the hospital-api UI; hospital-api references `asset_id`, never owns a parallel asset register. Depreciation accounting is already wired via treasury-api's `FixedAssetDepreciation`.
11. **Billing & Insurance** — per-encounter charges aggregated into a treasury invoice; SHA/SHIF/NHIF eligibility verification and claims submission via treasury-api's existing insurance connector; KRA eTIMS transmission (treasury-owned) is an **opt-in per tenant/service**, not applied to every encounter by default — many clinical services are not required to carry a fiscal invoice.
12. **Specialized care programmes** — ANC, PNC, ART, TB, Immunization tracking (MOH-reporting aligned), Morgue management.
13. **KHIS/DHIS2 aggregate reporting** — indicator export via the ADX standard for public/donor-funded programmes (ART, TB, immunization) that must report into Kenya's national KHIS, distinct from SHA/Taifa Care insurance-claims reporting.
14. **CSSD (sterilization) & Dietary** — lightweight tracking modules for sub-departments that support inpatient care.
15. **Patient communications** — SMS/WhatsApp appointment reminders, lab-result-ready, prescription-ready alerts via notifications-api.
16. **Reporting** — occupancy, revenue, and clinical-throughput dashboards (delegating financial aggregation to treasury-api, never re-summing).
17. **Compliance & audit** — Kenya Data Protection Act-aligned consent capture, audit trail, and retention policy for sensitive health data.

**Explicitly out of scope for Codevertex Afya v1** (deferred, not silently dropped): full PACS/RIS radiology image storage (an integration point to a third-party PACS via DICOM, not stored in hospital-api itself), payroll/HR duty rostering (already owned by `erp-api` — hospital-api references staff via `auth_service_user_id` like every other service), and full facilities/security/visitor-management (out of scope for a health-records system).

---

## References

- [Architecture](architecture.md)
- [Integrations](integrations.md)
- [Entity Relationship Diagram](erd.md)
- [pos-api Pharmacy Migration Plan](migration-pos-pharmacy.md)
- [Sprint Plans](sprints/)
- `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md` — canonical cross-service data-ownership matrix
- `shared-docs/TRINITY-AUTHORIZATION-PATTERN.md` — RBAC + Licensing + Resources authorization model
- `shared-docs/event-architecture.md` — uniform event-subject convention and service event catalog
- `d:\Projects\Codevertex\CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS PRICING MODEL.md` — Codevertex Afya tiered pricing
