# Hospital API — Architecture

**Last updated:** 2026-07-31 — Initial scaffold audit note: this document describes the *target*
layered architecture and data-authority boundaries for hospital-api. Only the platform-wiring layer
(config/logging/db/redis/nats/health/auth) is implemented so far; the domain layers below are planned.

---

## Design Philosophy

hospital-api gives a health facility **one connected patient record** across reception, consultation,
laboratory, pharmacy, inpatient, and billing — replacing the paper-file-plus-disconnected-registers
pattern common in Kenyan facilities today. It follows the same architectural conventions as every
other Codevertex Go service (chi + ent + Atlas + Postgres + Redis + NATS outbox) so it plugs into the
existing platform without inventing new patterns, and it **reuses rather than duplicates** the clinical
building blocks that already exist elsewhere in the ecosystem (see Data Authority table below).

## Supported Use Cases

| Facility type | What they use |
|---|---|
| Standalone chemist/dispensary (Pharmacy-only module toggle, below Afya Clinic) | Pharmacy dispensing + OTC sale only — no Consultation/Lab/Inpatient. Direct replacement for the retired pos-api "Dawa" use-case (see `migration-pos-pharmacy.md`) |
| Dispensary / health centre (Afya Clinic tier) | Reception, Consultation, Pharmacy, Billing, referred-out lab |
| Sub-county hospital (Afya Facility tier) | + in-house Laboratory, Inpatient, SHA/SHIF+NHIF claims, controlled-substance register |
| County referral / large private hospital (Afya Hospital tier) | + Theatre/OT, ICU, Blood Bank, Ambulance dispatch, Asset/Biomedical-equipment tracking, Maternity/Morgue, specialized programmes (ANC/PNC/ART/TB/Immunization), KHIS/DHIS2 reporting, multi-branch |

## Layer Overview

| Layer | Responsibility | Key paths |
|---|---|---|
| HTTP | Routing, middleware (tenant/outlet context, CORS, auth), request/response DTOs | `internal/http/{handlers,router}` |
| Service/Module | Business logic per domain (patient, triage, lab, pharmacy, billing) | `internal/modules/<domain>/` (planned, none yet) |
| Data | ent schemas + Atlas versioned migrations | `internal/ent/schema/` (empty — see its `README.md`) |
| Platform | Infra wiring: Postgres pool, Redis client, NATS connection, outbox | `internal/platform/{database,cache,events}` |
| Event | Transactional outbox → NATS JetStream (`shared-events`) | planned: `internal/ent/schema/outboxevent.go` + `shared-events` publisher, mirroring `library-service/library-api` |

## Data Authority (owns vs. references)

**Last revised:** 2026-07-31 (Round 2 — added Theatre/ICU/Blood Bank/Ambulance-reference/Asset-reference rows after the expanded department-catalog research; see the plan's Round 2 section).

hospital-api **owns**:
- `Patient`, `PatientVisit`/Encounter, `TriageRecord`, `ExaminationRecord`
- `DiagnosisCatalog` (tenant-custom entries only — the standard/default catalogue is global reference data, see `erd.md` Conventions)
- `LabOrder`/`LabOrderLine` (the `LabTest` catalogue itself is global reference data + tenant-custom additions)
- `Prescription`/`PrescriptionLine`, `ControlledSubstanceLog` — **the only place this logic lives on the platform**; see `migration-pos-pharmacy.md` for why pos-api carries none of it
- `Ward`/`Bed`/`Admission`, discharge summaries
- `TheatreBooking` (OT scheduling), `ICUEpisode` (critical-care monitoring flags)
- `DonorRecord`, `CrossmatchRequest`, `TransfusionRecord` (clinical blood-bank records — physical blood units are inventory-api lots, not owned here)
- `AmbulanceBooking` (thin reference row only — see below, not a dispatch/fleet engine)
- `Appointment`/OPD queue, `Referral`
- Specialized-care programme records: ANC, PNC, ART, TB, Immunization, Morgue
- KHIS/DHIS2 aggregate-reporting export configuration/history (the indicators themselves are computed from owned clinical data, not duplicated elsewhere)

hospital-api **references, never duplicates** (per `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`):

| Domain | Owner | How hospital-api accesses it |
|---|---|---|
| Drug/item master, lot/expiry, drug-interaction rules, controlled-substance schedule, KRA eTIMS item codes | `inventory-api` | REST via `shared/service-client`, references `inventory_item_id`/`lot_id` |
| **Blood-unit stock (as a lot-tracked, short-shelf-life item category)** | `inventory-api` | Same `InventoryLot` mechanism as drugs — no bespoke blood inventory system; hospital-api's Blood Bank module references `lot_id` on `TransfusionRecord` |
| **Biomedical equipment / hospital assets (beds, ventilators, ambulances-as-capital-assets), maintenance schedules** | `inventory-api` (**already implemented** — `Asset`/`AssetMaintenance` ent schemas) | REST, references `asset_id`; hospital-api surfaces this as "Biomedical Equipment" in its UI without owning a parallel register |
| **Asset depreciation accounting** | `treasury-api` (**already implemented** — `FixedAssetDepreciation`, references inventory-api's `asset_id`) | Read-only display; hospital-api never posts depreciation itself |
| **Ambulance/fleet vehicles, drivers, dispatch tasks, distance-based pricing** | `logistics-api` (**already implemented** — `FleetMember`, `Task` with a free-string `task_type` field, `PricingRule` with `rule_type: "distance"`) | hospital-api creates a `Task` with `task_type: "ambulance_dispatch"` (an additive string value — **zero schema change needed** in logistics-api) exactly like `ordering-backend` creates delivery tasks; `AmbulanceBooking` stores only the returned `logistics_task_id` |
| Invoices, quotations, insurance claims/coverage/remittance (SHA/SHIF/NHIF, future Taifa Care HMIS), payments, KRA eTIMS transmission (opt-in per tenant) | `treasury-api` | REST via `shared/service-client`, references `invoice_id`/`payment_intent_id`/`insurance_claim_id` |
| Tenant/user identity, roles, global role/permission catalogue | `auth-api` | JWT (JWKS) validated via `shared-auth-client`; `tenant_id`/`user_id` references only |
| Hospital `service_tag` subscription plans/tiers/entitlements (incl. the standalone-chemist module-toggle configuration) | `subscriptions-api` | JWT `sub_*` claims + REST fallback |
| SMS/WhatsApp patient reminders | `notifications-api` | Outbox event → notifications-api consumer (never a direct SMS/email send from hospital-api) |

**Entities that must NOT exist in hospital-api**: item/drug master rows, `InventoryLot`, `DrugInteractionRule`, `Asset`/`AssetMaintenance` (all inventory-api); `Invoice`, `Quotation`, `InsuranceClaim`/`InsurancePlan`/`InsuranceProvider`, `FixedAssetDepreciation` (all treasury-api); fleet/vehicle/driver records, dispatch task lifecycle, pricing-rule engines (all logistics-api — hospital-api's `AmbulanceBooking` is a reference row, not a competing fleet system); full user/tenant profile rows beyond the minimal JIT reference (auth-api). If any of these ever appear as local tables, that is a data-ownership violation — remove and replace with a reference ID + S2S call.

**Entities that must NOT exist in pos-api** (post-migration, see `migration-pos-pharmacy.md`): `Patient`, `PatientVisit`, `TriageRecord`, `ExaminationRecord`, `LabOrder`/`Line`, `Prescription`/`Line`, `ControlledSubstanceLog`, `DrugInteractionCheck` — pos-api's remaining use-cases are exactly `retail`, `hospitality`, `quick_service`, `services`.

## Runtime Document Generation (future)

Any hospital-api-owned PDF (lab report, discharge summary, prescription/dispensing label, patient
statement) must adopt the platform's proven **`treasury-api/internal/modules/docs` `go-pdf/fpdf`
engine** (`docs.Document` model + per-type `adapters/`), the same way `erp-api` adopted it for
payslips/payroll reports — never a new ad-hoc PDF stack. This is distinct from the one-off,
Markdown-driven `shared-docs/hospital-quotation/` pipeline used to generate the *business-development*
client quotation PDF, which is not a runtime API feature.

## Trinity Authorization Wiring

hospital-api follows the platform's three-layer authorization model (`shared-docs/TRINITY-AUTHORIZATION-PATTERN.md`):

1. **RBAC (auth-api)** — global roles/permissions in the JWT (`superuser`, `admin`, `manager`, `staff`, `doctor`, `nurse`, `pharmacist`, `records_clerk`, ...). Implemented via `shared-auth-client`'s JWKS validator + `AuthMiddleware.RequireAuth` (see `internal/http/router/router.go`).
2. **Licensing (subscriptions-api)** — `service_tag: "hospital"` plan/tier entitlements (`AFYA_CLINIC`/`AFYA_FACILITY`/`AFYA_HOSPITAL`) embedded as `sub_*` JWT claims. **Shipped 2026-08-01**: `internal/platform/subscriptions/{client,gate,features}.go`, `SubscriptionGate()` wired in the router chain, mutations-only, fails open on lookup failure.
3. **Resources (hospital-api itself)** — fine-grained `hospital.{module}.{action}` permission codes in a local RBAC module (`internal/modules/rbac`), JIT-provisioned from JWT claims via `internal/modules/identity` (self-heals role assignment on every authenticated request, not just first login), global role catalogue (no `tenant_id` on the role tables — see `erd.md` Conventions). **Shipped 2026-08-01** as plumbing; no domain permission codes exist yet beyond the seed set, since there are no domain modules to gate (Sprint 4+).

Multi-outlet/branch support (for Afya Hospital tier multi-branch tenants) uses the standard `X-Outlet-ID` header + `httpware.WithOutletID` context pattern, optional and additive — absent means tenant-wide data.

## Changelog

- **2026-07-31** — Initial architecture doc written alongside the Sprint-0 scaffold. Layer overview, data-authority table, and Trinity wiring plan established ahead of any domain code.
- **2026-08-01** — Trinity wiring plan executed: RBAC/JIT identity/tenant+outlet sync/subscription
  gating all shipped (see `docs/integrations.md` §3/§4 for the up-to-date status). Still no
  clinical domain schemas — that's `docs/migration-pos-pharmacy.md` Phase A / Sprint 4.
