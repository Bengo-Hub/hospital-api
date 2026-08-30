# Hospital API — Architecture

**Last updated:** 2026-08-29 — Sprints 1, 2, 5-core, 3, 4 shipped real domain code (Patient/OPD/
Triage, Consultation/Examination, the Billing ledger, Laboratory, Pharmacy/Dispensing) on top of
the platform-wiring layer described below. This document's data-authority boundaries and the
Distributed Billing design were written ahead of that code and are confirmed accurate against it;
see the Layer Overview and Changelog for what is now real versus still planned (Sprints 6-13).

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
| Dispensary / health centre (Afya Clinic tier) | Reception, Triage, Consultation, Pharmacy, Billing, referred-out lab |
| Sub-county hospital (Afya Facility tier) | + in-house Laboratory, Inpatient, SHA/SHIF+NHIF claims, controlled-substance register |
| County referral / large private hospital (Afya Hospital tier) | + Theatre/OT, ICU, Blood Bank, Ambulance dispatch, Asset/Biomedical-equipment tracking, Maternity/Morgue, specialized programmes (ANC/PNC/ART-OTZ/TB/Immunization/VMMC/PMTCT-EID/cancer screening), KHIS/DHIS2 reporting, multi-branch |

## Layer Overview

| Layer | Responsibility | Key paths |
|---|---|---|
| HTTP | Routing, middleware (tenant/outlet context, CORS, auth), request/response DTOs | `internal/http/{handlers,router}` |
| Service/Module | Business logic per domain (patient, triage, lab, pharmacy, billing) | `internal/modules/{patients,consultation,lab,pharmacy,billing,refdata,sequence,...}/` — real, shipped 2026-08-29 for Sprints 1/2/3/4/5-core; Sprints 6+ still planned |
| Data | ent schemas + Atlas versioned migrations | `internal/ent/schema/` — real schemas + generated migrations for Sprints 1-5-core (Patient/PatientVisit/TriageRecord/Referral/ExaminationRecord/DiagnosisCatalog*/LabOrder*/LabTestCatalog*/Prescription*/ControlledSubstanceLog/DrugInteractionCheck/BillableItemCatalog/PatientAccount/BillableCharge/PatientNextOfKin/OutboxEvent/DocumentSequence); Sprints 6+ tables still planned |
| Platform | Infra wiring: Postgres pool, Redis client, NATS connection, outbox | `internal/platform/{database,cache,events}` |
| Event | Transactional outbox → NATS JetStream (`shared-events`) | shipped 2026-08-29: `internal/ent/schema/outbox_event.go` + `internal/events/publish.go` + `shared-events` `OutboxPoller` wired in `internal/app/app.go`, publishing `hospital.patient.created`/`hospital.visit.admitted`/`hospital.lab_order.resulted` |

## Data Authority (owns vs. references)

**Last revised:** 2026-07-31 (Round 2 — added Theatre/ICU/Blood Bank/Ambulance-reference/Asset-reference rows after the expanded department-catalog research; see the plan's Round 2 section).

hospital-api **owns**:
- `Patient`, `PatientVisit`/Encounter, `TriageRecord`, `ExaminationRecord`
- `DiagnosisCatalog` (tenant-custom entries only — the standard/default catalogue is global reference data, see `erd.md` Conventions)
- `LabOrder`/`LabOrderLine` (the `LabTest` catalogue itself is global reference data + tenant-custom additions)
- `Prescription`/`PrescriptionLine`, `ControlledSubstanceLog` — **the only place this logic lives on the platform**; see `migration-pos-pharmacy.md` for why pos-api carries none of it
- `BillableItemCatalog`, `PatientAccount`, `BillableCharge`, `PatientNextOfKin` — the **billing ledger** (what's owed, by which department, settled or not). This is distinct from the actual money: hospital-api never stores an invoice/payment/GL row (those stay treasury-owned below) — it owns the clinical-workflow question of "has this step been paid for," the same relationship pos-api's `POSOrder` already has to treasury. See "Distributed Billing & Patient Accounts" below.
- `Ward`/`Bed`/`Admission`, discharge summaries
- `TheatreBooking` (OT scheduling), `ICUEpisode` (critical-care monitoring flags)
- `DonorRecord`, `CrossmatchRequest`, `TransfusionRecord` (clinical blood-bank records — physical blood units are inventory-api lots, not owned here)
- `AmbulanceBooking` (thin reference row only — see below, not a dispatch/fleet engine)
- `Appointment`/OPD queue, `Referral`
- Specialized-care programme records: ANC, PNC, ART (with an Operation Triple Zero adolescent-cohort
  flag), TB, Immunization, VMMC, HIV-Exposed Infant/PMTCT follow-up, cervical and prostate cancer
  screening, Morgue (expanded 2026-08-29 per `docs/kenyaemr-technical-reference.md` §4, which found
  these are the real programmes Kenya's dominant clinical EMR tracks as distinct modules)
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

## Distributed Billing & Patient Accounts (2026-08-29 research round)

**The problem this section solves**: a naive "checkout at the very end of the visit" design (the
original Sprint 5 draft) forces a real patient loop — records charges a registration fee, sends the
patient to a single central cashier to pay, back to triage/consultation, doctor orders labs, back to
the cashier again to pay for those, back to lab, results return to the doctor, doctor prescribes,
patient goes to pharmacy which either collects payment itself or sends the patient back to the
cashier a third time. Every department already knows what it's charging for at the moment it charges
it — routing 100% of collection through one physical window is what actually causes the walking back
and forth, not a technical limitation.

**External grounding**: KenyaEMR (OpenMRS-based, 1450+ Kenyan facilities, the platform's own home
market) models exactly this as **"Billable Service Configurations"** — a per-service price/department
catalog distinct from the actual cash-collection point. General 2026 hospital-billing research
confirms the same two patterns independently: (1) inpatient billing runs as a **running account that
accrues charges by department as they occur** (room/day rate, ward consumables, diagnostics, pharmacy
issues) rather than a single end-of-stay reconstruction, reviewable mid-stay; (2) decentralizing
payment collection to the point of care/order, rather than funneling every patient through one
central cashier window, is an established pattern for cutting queue time — some deployments report
double-digit-minute waiting-time reductions from moving payment closer to the point of service.

**The model**: a **billing ledger, not a billing counter**. Every department can charge; not every
department has to collect.

- `BillableItemCatalog` (tenant-configured, seeded with sane defaults per facility tier): one row per
  chargeable service — `department` (records/triage/consultation/lab/pharmacy/theatre/inpatient/
  mortuary), `code`, `name`, `price` (fixed, or `variable` for anything inventory-api prices, e.g.
  drugs/lab consumables), `applies_to` (`first_visit`/`return_visit`/`all` — registration fee
  variants), `requires_prepayment` (bool — must be settled before the associated clinical step may
  proceed, e.g. lab tests, mirroring pos-api's existing `ActivateLabOrderIfPaid` gate), and
  `collection_mode` (`direct` | `billing_queue` | `either`) — whether the originating department may
  collect payment itself or must hand off to the Billing desk. Configurable per tenant, defaults
  sensibly per facility type (see below).
- `PatientAccount`: one running ledger per Patient (spans every visit for an inpatient admission;
  effectively one per OPD visit for outpatient, since OPD settles per-visit). Tracks
  `total_charged`/`total_paid`/`balance`, `status` (`open`/`settled`/`written_off`), and
  `settlement_required_before` (`nothing`/`discharge`/`body_release`) — the hard gate for
  admission/mortuary workflows.
- `BillableCharge`: one row per charge event — `patient_account_id`, `billable_item_id`,
  `source_module`/`source_reference_id` (the `LabOrder`/`Prescription`/`Admission` etc. that
  generated it), `amount`, `status` (`pending`/`invoiced`/`paid`/`waived`/`written_off`),
  `treasury_invoice_id`/`treasury_payment_intent_id` (nullable — filled in once ANY authorized
  department actually collects), `created_by_department`, `paid_at`.
- `PatientNextOfKin`: `name`, `phone`, `relationship`, `id_number`, `is_primary` — who may settle a
  bill or authorize release on the patient's behalf when the patient isn't the one paying (discharge
  of a minor/incapacitated patient, or mortuary release). Recorded against the settling
  `BillableCharge`/payment for audit, not a login identity.

**Collection is permission-gated, not location-gated.** Any department whose staff hold
`hospital.billing.collect_own` may collect payment for a charge THEY created, using the same
underlying "collect payment" primitive (creates a treasury payment intent/invoice via the existing
S2S client — no new treasury-api work, see `sprint-5-billing-insurance.md`) that the Billing desk
itself uses. The Billing/cashier role additionally holds `hospital.billing.collect_any` and owns a
"Pending Charges" queue across every department/patient — the fallback for any department that
doesn't want to handle collection itself, and the default handler for the first-contact fees
(registration, consultation) where a dedicated desk naturally sits. **Defaults by facility type**
(overridable per tenant): Chemist → Billing module is just Walk-in Sale, no `PatientAccount`
complexity at all (same as today's pos-api pharmacy "direct" checkout). Clinic/health-centre →
registration+consultation default to `billing_queue` (Billing desk), pharmacy defaults to `direct`
(small team, one person often does both). Facility/Hospital → every department capable of
`direct`, Billing desk is the explicit fallback and the inpatient billing owner.

**"Receipt accessible to every department" is the ledger, not a physical slip.** Any authenticated
department, given a patient/visit ID, calls `GET /patients/{id}/account` and sees the live
paid/unpaid status of every charge instantly — this is what lets triage/lab/pharmacy check "has this
been paid?" without the patient needing to physically carry proof between desks. A printed receipt
remains available as a convenience/backup for facilities with weak connectivity or patients who want
one, but the digital ledger is authoritative — a lost paper slip cannot desync it.

**Validated against KenyaEMR's real architecture (2026-08-29):** a direct technical audit of
KenyaEMR's production billing code confirms this distributed model is a genuine advance over the
market's most-deployed open-source system, not just a design preference. KenyaEMR's actual billing
backend (the legacy OpenHMIS "Cashier" module) ties one bill to one physical `CashPoint` and one
cashier `Provider` at posting time, a cash-point-centric model, the opposite of the any-department-
can-charge design above. Its separate, actively-maintained insurance-claims module does validate part
of our own design, though: a claim there is built by selecting already-posted bill line-items and
attaching `claimCode`/`guaranteeId`/`claimExplanation`/`claimJustification`, diagnoses, provider(s),
and a treatment date range, the same "claim references already-posted charges, never re-derives them"
shape `docs/integrations.md` §2.2 and `sprint-5-billing-insurance.md` already assume. It also names a
useful status this platform's own `BillableCharge.status` enum lacks: `EXEMPTED`, distinct from
`waived`, for a charge insurance covered in full. Worth adding as a status value when Sprint 5 is
implemented. Full detail: `docs/kenyaemr-technical-reference.md` §3.

**Discharge/mortuary settlement gate**: discharge/body-release checks `PatientAccount.balance <= 0`.
If outstanding, the action surfaces Record Payment / Apply Insurance / Write-Off options right there
(next-of-kin can be the one who pays — their identity is recorded on the settling charge, not
required to exist as a system user) rather than blocking silently. An explicit
`hospital.billing.override_settlement` permission + mandatory reason lets an authorized user release
a patient/body with an outstanding balance (real facilities do this for emergencies/charity cases) —
same "guardrail with an audited approval escape hatch" convention the platform already uses
elsewhere (pos-api's `ApprovalIntentID` pattern), never a silent bypass.

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
3. **Resources (hospital-api itself)** — fine-grained `hospital.{module}.{action}` permission codes (~40, spanning every shipped module) in a local RBAC module (`internal/modules/rbac`), JIT-provisioned from JWT claims via `internal/modules/identity` (self-heals role assignment on every authenticated request, not just first login). `HospitalUser` identity is scoped per `(tenant_id, auth_service_user_id)` — fixed 2026-08-30, see the Changelog and the User Management Module section below; a prior version made the same auth-service user's row platform-wide-unique, corrupting data for anyone belonging to more than one hospital tenant. `HospitalRole` is global by default (no `tenant_id`) with a documented copy-on-write exception for tenant customization — see below and `erd.md` Conventions.

Multi-outlet/branch support (for Afya Hospital tier multi-branch tenants) uses the standard `X-Outlet-ID` header + `httpware.WithOutletID` context pattern, optional and additive — absent means tenant-wide data. No per-user outlet-membership restriction exists yet — any authenticated tenant user may select any of the tenant's outlets via this header (`internal/http/middleware/outlet_context.go`'s own doc comment flags this as a known, deliberately deferred gap, out of scope for the user-management work below since it's a different axis — outlet access, not role/permission management).

## User Management Module (2026-08-30)

A full audit of the original 3-endpoint Users surface (list users, list global roles, set one
primary role) found real gaps and one live-prod-risk bug; all closed the same day:

- **Tenant-scoped identity fix** — `HospitalUser` no longer reuses the auth-service user's own
  UUID as its primary key with a platform-wide-unique `auth_service_user_id`; it's now a locally
  generated UUID unique only per `(tenant_id, auth_service_user_id)`, so the same person can
  correctly hold a row in more than one hospital tenant. Every JWT-`sub`-to-local-ID resolution
  point (`middleware/permission.go`, `auth_me.go`) now goes through a `rbac.UserResolver`
  (`identity.Service.ResolveLocalUserID`) instead of assuming ID equality.
- **Copy-on-write role customization** — `HospitalRole` gained a nullable `tenant_id` (NULL =
  global) and `cloned_from_role_id`. `rbac.Service.CustomizeRole` idempotently clones a global
  role into a tenant-scoped copy on first edit (re-pointing existing assignments so it applies
  immediately); `CreateCustomRole` builds a from-scratch tenant-only role; `UpdateRolePermissions`
  edits a tenant-owned role (global rows are never directly mutable).
- **`RbacAuditLog`** — a minimal, additive audit trail (actor, action, before/after) for every
  RBAC mutation, deliberately named/scoped apart from Sprint 12's future compliance-grade
  `audit_log`. No FK edges, so entries survive their target being hard-deleted.
- **Deactivate/reactivate** — `identity.Service.SetUserStatus`, enforced at the same
  `ResolveLocalUserID` choke point (a non-active user resolves to "no local access").
- **Additive multi-role** — `AssignExtraRole`/`RevokeExtraRole` grant/revoke a supplemental role
  without disturbing the primary one (`SetUserRole`'s existing "exactly one primary role"
  contract is unchanged and still wipes extras on a primary-role change — documented, not fixed).
- **`expires_at` enforcement** — was stored on `UserRoleAssignment` but never checked; now
  filtered at the two read choke points (`GetUserRoles`/`ListUserAssignments`), no background
  sweep needed.
- **Real invite-staff flow** — `identity.Service.InviteMember` relays to auth-api's own S2S
  `POST /api/v1/s2s/tenants/{tenant_id}/members` (the same mechanism every other service's own
  staff-invite flow uses — see `docs/integrations.md` §3), rather than inventing a new identity
  path.
- Full HTTP surface: `GET /permissions`, `GET/PUT /roles/{id}/permissions`, `POST /roles`,
  `POST /roles/customize`, `PUT /users/{id}/status`, `POST/DELETE /users/{id}/roles[/{code}]`,
  `POST /users/invite`, `GET /audit-log` (paginated via `github.com/Bengo-Hub/pagination`).

Two opt-in live-verification test files (`internal/modules/rbac/role_customization_live_test.go`,
`internal/modules/identity/rbac_lifecycle_live_test.go`, gated on `VERIFY_POSTGRES_URL`, skipped
in CI) exercise all of the above against a real local Postgres — this service has no sqlite/CGO
test harness, so this is the only pre-deploy check for a bad migration or query.

**Not built, by deliberate scope decision, not oversight**: per-user outlet/branch membership
enforcement (a distinct "outlet access control" initiative, see the outlet paragraph above) and a
role-deletion endpoint (no `DeleteRole`/frontend delete action exists — a role can be created and
edited but not removed; a small, low-risk follow-up if ever needed). `hospital.config.manage`'s
existing "no handler" state was verified to be a deliberate, already-documented architectural
decision (see `internal/http/handlers/config.go`'s own doc comment) — facility identity/branding
is auth-api's, and `facility_type`/`enabled_modules` is a read-only subscriptions-api cache, so
there is genuinely nothing hospital-api-specific for a tenant admin to write there today.

## Changelog

- **2026-07-31** — Initial architecture doc written alongside the Sprint-0 scaffold. Layer overview, data-authority table, and Trinity wiring plan established ahead of any domain code.
- **2026-08-01** — Trinity wiring plan executed: RBAC/JIT identity/tenant+outlet sync/subscription
  gating all shipped (see `docs/integrations.md` §3/§4 for the up-to-date status). Still no
  clinical domain schemas — that's `docs/migration-pos-pharmacy.md` Phase A / Sprint 4.
- **2026-08-29** — KenyaEMR technical audit (`docs/kenyaemr-technical-reference.md`) validated the
  Distributed Billing design against KenyaEMR's real cash-point-centric billing code, and surfaced a
  useful `EXEMPTED` charge-status value to add at Sprint 5. Specialized-care programme list expanded
  to include VMMC, an OTZ adolescent-ART cohort flag, PMTCT/EID follow-up, and cervical/prostate
  cancer screening, the real programmes Kenya's dominant clinical EMR tracks as distinct modules.
- **2026-08-29 (later same day)** — Sprints 1, 2, 5-core, 3, 4 implemented in code, in that build
  order (`hospital-api@05741fd`/`709b140`/`126adbf`/`878e0ce`/`4005c21`, on top of Phase-0 groundwork
  `19ad7cb`/`024a297`): Patient/OPD/Triage/Referral, Consultation/Examination/Diagnosis-Catalogue
  (confirmed ICD-11, seeded via the new `internal/modules/refdata` package), the Billing ledger
  (`BillableItemCatalog`/`PatientAccount`/`BillableCharge`/`PatientNextOfKin` + collect/queue/settle/
  override-settlement), Laboratory (`requires_prepayment` gating live against the ledger), and
  Pharmacy/Dispensing (FEFO-aware dispense via the fixed `ConsumeReservation`, controlled-substance
  dual-witness log, per-line `BillableCharge` posting). Also found+fixed two real infra gaps
  (`cmd/migrate`/`cmd/seed` were both total no-op stubs) and one real RBAC seed idempotency bug
  (`SeedRoles`'s fast-path meant permission codes added after first seed silently never landed in an
  already-provisioned environment, including prod). Full detail:
  `.claude/plans/pharmacy-to-hospital-service-migration-2026-08-29.md`. Not yet done: insurance
  eligibility/claim wiring into an actual checkout flow, `BillableItemCatalog` seed data, hospital-ui,
  and any live E2E run against a running server.
- **2026-08-29 (Sprint 5 remainder)** — the three items the previous entry flagged as outstanding
  are now done, except hospital-ui and a live E2E run. Insurance: `billing.Service` gained
  `CheckEligibility`/`SubmitInsuranceClaim`/`PollInsuranceClaim` wrapping the Phase-0 treasury
  client, called from a lab-order-scoped and a prescription-scoped insurance-claim action (the
  actual clinical wiring points — a `requires_prepayment` lab order or a dispensed drug can now be
  settled by an accepted claim instead of cash) plus the generic visit-level proxy routes the
  sprint doc originally specified. `BillableCharge.status` gained the `exempted` value flagged in
  the KenyaEMR-audit changelog entry above; `lab.ActivateIfPaid` now treats `paid` and `exempted`
  as equally "settled." No Atlas migration file was generated for the enum addition — this repo's
  ent-to-Postgres mapping stores every `field.Enum` as a plain `character varying` with no DB-level
  CHECK constraint (confirmed against every existing migration file), so there is genuinely no SQL
  diff to migrate; this was verified by actually running the ent versioned-migration diff against a
  scratch local Postgres, not assumed from reading the schema. `BillableItemCatalog`: added
  `refdata.SeedFacilityBillableItems` (a real per-facility-tier starter price list, matching the
  schema doc's own forward-reference to this exact function name) called idempotently from
  `tenant.Syncer.SyncTenant`, resolving `facility_type` via the existing
  `subscriptions.Client.GetEntitlements` lookup; plus a tenant admin CRUD surface gated on the new
  `hospital.billing.manage_catalog` permission. Full detail:
  `docs/sprints/sprint-5-billing-insurance.md`.
- **2026-08-30** — User Management Module rebuild (see the new section above): tenant-scoped
  `HospitalUser` identity fix (a real prod-data-integrity bug, live-verified against a local
  Postgres sandbox before shipping), copy-on-write role customization, `RbacAuditLog`,
  deactivate/reactivate, additive multi-role, `expires_at` enforcement, and a real invite-staff
  flow via auth-api's S2S member endpoint. hospital-api commits
  `3d626ac`/`97f0614`/`dcbc3db`/`250cdf4`; matching hospital-ui pages in the same-day commit
  `5c4dead`.
