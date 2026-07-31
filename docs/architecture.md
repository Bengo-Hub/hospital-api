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
| Dispensary / health centre (Afya Clinic tier) | Reception, Consultation, Pharmacy, Billing, referred-out lab |
| Sub-county hospital (Afya Facility tier) | + in-house Laboratory, Inpatient, SHA/SHIF+NHIF claims, controlled-substance register |
| County referral / large private hospital (Afya Hospital tier) | + Theatre/Maternity/Morgue, specialized programmes (ANC/PNC/ART/TB/Immunization), multi-branch |

## Layer Overview

| Layer | Responsibility | Key paths |
|---|---|---|
| HTTP | Routing, middleware (tenant/outlet context, CORS, auth), request/response DTOs | `internal/http/{handlers,router}` |
| Service/Module | Business logic per domain (patient, triage, lab, pharmacy, billing) | `internal/modules/<domain>/` (planned, none yet) |
| Data | ent schemas + Atlas versioned migrations | `internal/ent/schema/` (empty — see its `README.md`) |
| Platform | Infra wiring: Postgres pool, Redis client, NATS connection, outbox | `internal/platform/{database,cache,events}` |
| Event | Transactional outbox → NATS JetStream (`shared-events`) | planned: `internal/ent/schema/outboxevent.go` + `shared-events` publisher, mirroring `library-service/library-api` |

## Data Authority (owns vs. references)

hospital-api **owns**:
- `Patient`, `PatientVisit`/Encounter, `TriageRecord`, `ExaminationRecord`
- `DiagnosisCatalog` (tenant-custom entries only — the standard/default catalogue is global reference data, see `erd.md` Conventions)
- `LabOrder`/`LabOrderLine` (the `LabTest` catalogue itself is global reference data + tenant-custom additions)
- `Prescription`/`PrescriptionLine`, `ControlledSubstanceLog`
- `Ward`/`Bed`/`Admission`, discharge summaries
- `Appointment`/OPD queue, `Referral`
- Specialized-care programme records: ANC, PNC, ART, TB, Immunization, Morgue

hospital-api **references, never duplicates** (per `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`):

| Domain | Owner | How hospital-api accesses it |
|---|---|---|
| Drug/item master, lot/expiry, drug-interaction rules, controlled-substance schedule, KRA eTIMS item codes | `inventory-api` | REST via `shared/service-client`, references `inventory_item_id`/`lot_id` |
| Invoices, quotations, insurance claims/coverage/remittance (SHA/SHIF/NHIF, future Taifa Care HMIS), payments, KRA eTIMS transmission (opt-in per tenant) | `treasury-api` | REST via `shared/service-client`, references `invoice_id`/`payment_intent_id`/`insurance_claim_id` |
| Tenant/user identity, roles, global role/permission catalogue | `auth-api` | JWT (JWKS) validated via `shared-auth-client`; `tenant_id`/`user_id` references only |
| Hospital `service_tag` subscription plans/tiers/entitlements | `subscriptions-api` | JWT `sub_*` claims + REST fallback |
| SMS/WhatsApp patient reminders | `notifications-api` | Outbox event → notifications-api consumer (never a direct SMS/email send from hospital-api) |

**Entities that must NOT exist in hospital-api**: item/drug master rows, `InventoryLot`, `DrugInteractionRule` (all inventory-api); `Invoice`, `Quotation`, `InsuranceClaim`/`InsurancePlan`/`InsuranceProvider` (all treasury-api); full user/tenant profile rows beyond the minimal JIT reference (auth-api). If any of these ever appear as local tables, that is a data-ownership violation — remove and replace with a reference ID + S2S call.

## Runtime Document Generation (future)

Any hospital-api-owned PDF (lab report, discharge summary, prescription/dispensing label, patient
statement) must adopt the platform's proven **`treasury-api/internal/modules/docs` `go-pdf/fpdf`
engine** (`docs.Document` model + per-type `adapters/`), the same way `erp-api` adopted it for
payslips/payroll reports — never a new ad-hoc PDF stack. This is distinct from the one-off,
Markdown-driven `shared-docs/hospital-quotation/` pipeline used to generate the *business-development*
client quotation PDF, which is not a runtime API feature.

## Trinity Authorization Wiring

hospital-api follows the platform's three-layer authorization model (`shared-docs/TRINITY-AUTHORIZATION-PATTERN.md`):

1. **RBAC (auth-api)** — global roles/permissions in the JWT (`superuser`, `admin`, `manager`, `staff`, ...). Implemented today via `shared-auth-client`'s JWKS validator + `AuthMiddleware.RequireAuth` (see `internal/http/router/router.go`).
2. **Licensing (subscriptions-api)** — `service_tag: "hospital"` plan/tier entitlements embedded as `sub_*` JWT claims (mutations-only enforcement, matching every sibling service). Not yet wired — planned for the sprint that adds the first mutating endpoint.
3. **Resources (hospital-api itself)** — fine-grained `hospital.{module}.{action}` permission codes (e.g. `hospital.prescriptions.dispense`, `hospital.lab_orders.result`) in a local RBAC module, JIT-provisioned from JWT claims, global role catalogue (no `tenant_id` on the role tables — see `erd.md` Conventions). Not yet implemented — planned alongside the first domain module (Sprint 1).

Multi-outlet/branch support (for Afya Hospital tier multi-branch tenants) uses the standard `X-Outlet-ID` header + `httpware.WithOutletID` context pattern, optional and additive — absent means tenant-wide data.

## Changelog

- **2026-07-31** — Initial architecture doc written alongside the Sprint-0 scaffold. Layer overview, data-authority table, and Trinity wiring plan established ahead of any domain code.
