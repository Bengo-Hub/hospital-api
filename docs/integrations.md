# Hospital API — Integration Guide

**Last updated:** 2026-07-31

> **Status note:** none of the S2S clients described below are implemented yet — this document
> specifies the target integration contracts (mirroring proven patterns already live in `pos-api`)
> so that Sprint 4/5 implement them correctly the first time, using `shared/service-client`
> (circuit breaker + retry + OTel) rather than a hand-rolled HTTP client — pos-api's
> `internal/modules/inventory/client.go` is a hand-rolled client and is explicitly **not** the
> pattern to copy; only its endpoint contracts are worth reusing.

---

## 1. Inventory Service Integration

hospital-api is a thin client of `inventory-api` for anything drug/item-master related. It must
never store its own copy of drug classification, lot/expiry, or interaction rules.

### 1.1 Drug master lookup & classification

**Client:** `internal/modules/inventory/client.go` (planned, via `shared/service-client`)
**Calls:** `GET /v1/{tenant}/inventory/items/{sku}`, `GET /v1/{tenant}/inventory/items?type=DRUG`
**Reads:** `generic_name`, `active_ingredient`, `dosage_form`, `strength`, `drug_class`,
`controlled_substance_schedule`, `is_controlled_substance`, `requires_age_verification`.
**hospital-api stores:** `inventory_item_id` (UUID FK) + snapshot fields on `PrescriptionLine`
(drug name, dose) captured at prescribing time — never the live master row.

### 1.2 Drug interaction / allergy check

**Calls:** `POST /v1/{tenant}/inventory/items/check-interactions` (same contract pos-api's
`CheckInteractions` already proves in production).
**Trigger:** every time a prescriber adds a line to a `Prescription`.

### 1.3 Lot/expiry-aware dispensing

**Calls:** `POST /v1/{tenant}/inventory/consumption` (FEFO lot consumption), reservations via
`POST /v1/{tenant}/inventory/reservations` + `/{id}/consume` — same contract as pos-api's pharmacy
checkout flow. `ConsumedLot` (lot number, expiry) is stored on `PrescriptionLine` as an audit
snapshot, not re-modeled locally.

### 1.4 Stock/expiry alerts (consumed)

**Subscribes to:** `inventory.lot.expiry_warning`, `inventory.stock.low` — surfaced to pharmacy
staff via a dashboard alert, not re-published as a new event type.

### 1.5 Biomedical equipment / hospital assets (already built — reuse, don't rebuild)

**Discovery (2026-07-31):** `inventory-api` already has `Asset` and `AssetMaintenance` ent schemas
(`inventory-service/inventory-api/internal/ent/{asset,assetmaintenance}.go`) covering asset tag,
category, serial/model/manufacturer, purchase/current/salvage value, depreciation rate+method, KRA
capital-allowance class, location, `outlet_id`, assigned-to/custodian, optional link to an
inventory `Item`, status/condition, warranty expiry, and a full maintenance-schedule +
`AssetMaintenance` history (type, scheduled/completed dates, performed-by, cost, findings, downtime
hours). This is exactly a biomedical-equipment/hospital-bed/ambulance-as-capital-asset register.

**Calls:** `GET /v1/{tenant}/inventory/assets`, `GET /v1/{tenant}/inventory/assets/{id}`,
`GET/POST /v1/{tenant}/inventory/assets/{id}/maintenance` (exact paths TBD when inventory-api's
asset handlers are confirmed — coordinate with inventory-api's own `docs/integrations.md`).
hospital-api's UI surfaces this data as "Biomedical Equipment" and lets clinical staff see e.g.
"ventilator X is due for maintenance" — it does **not** own a parallel asset table. Depreciation
accounting for the same asset is already wired via `treasury-api`'s `FixedAssetDepreciation`
(references `asset_id`) — hospital-api never posts depreciation.

### 1.6 Blood-unit stock (lot-tracked, no bespoke blood inventory)

Physical blood bags are modeled as a short-shelf-life, lot-tracked item category
(`InventoryLot`) in inventory-api, the same mechanism already used for drug batch/expiry. hospital-api's
Blood Bank module (`DonorRecord`, `CrossmatchRequest`, `TransfusionRecord`) references `lot_id` for
the physical unit and calls the same consumption/reservation endpoints pharmacy dispensing uses —
no second inventory system for blood.

---

## 2A. Logistics Service Integration — Ambulance & Emergency Dispatch

**ADR (2026-07-31):** Ambulance dispatch is a **thin reference into logistics-api**, not a new
fleet/dispatch/pricing engine inside hospital-api. Kenyan private-ambulance pricing (researched this
round) is a fixed call-out fee plus a per-km rate from base→call-out→hospital→return-to-base —
this maps exactly to logistics-api's existing `PricingRule` (`rule_type: "distance"`,
`distance_tiers` JSON). logistics-api's `Task.task_type` is a free-form string field (documented
values: `food_delivery | retail_delivery | outlet_transfer | commercial_courier | drop_shipping |
pickup | return | ride`) — adding `ambulance_dispatch` requires **zero schema/migration change** in
logistics-api, exactly the additive-metadata approach preferred over a new table.

**Flow:**
```
1. hospital-api calls POST /v1/{tenant}/tasks on logistics-api (S2S via shared/service-client)
   { task_type: "ambulance_dispatch", source_service: "hospital-service", pickup_location, ... }
2. logistics-api assigns a FleetMember tagged "ambulance" (specialization_tags), returns task_id
3. hospital-api stores logistics_task_id on its own AmbulanceBooking row (reference only)
4. hospital-api subscribes to logistics.task.assigned / logistics.task.completed for status updates
5. Billing: the distance-based fare from the completed task is passed to treasury-api as a line
   item on the patient's invoice, same as any other billable service (see § 2.1)
```

**Optional (Phase 3+):** a recurring "ambulance membership" product (individual/family annual plan,
mirroring St John Kenya's existing model) — billed as a treasury-api recurring/subscription charge,
not a new billing engine.

**hospital-api does NOT**: store rider/vehicle profiles, dispatch task lifecycle, or pricing rules —
all of that stays in logistics-api exactly as it does for every other service that dispatches tasks
(ordering-backend, pos-api).

---

## 2. Treasury Service Integration

hospital-api never owns financial documents or insurance data — treasury-api is the system of
record for money and tax, exactly as it is for every other Codevertex service.

### 2.1 Per-encounter billing

**Flow:**
```
1. hospital-api aggregates encounter charges (consultation fee, lab fee, drug dispense charge)
2. hospital-api calls treasury-api S2S: POST /api/v1/s2s/{tenant}/invoices
   (line items referencing inventory_item_id for drugs, flat fee codes for services)
3. treasury-api creates the Invoice, returns invoice_id
4. hospital-api stores invoice_id on the PatientVisit/Encounter as a reference
5. Patient pays via M-Pesa/card/insurance — treasury-api owns the payment intent lifecycle
```

### 2.2 SHA/SHIF/NHIF eligibility & claims (opt-in per tenant/service)

**Calls:** treasury-api's existing DAWA insurance connector — `ListPatientCoverages`,
`CheckInsuranceEligibility`, claim submission endpoints (`internal/modules/insurance/*` on
treasury-api) — same pattern as pos-api's `internal/modules/treasury/client.go`. **Not every
encounter requires this** — many clinical services do not carry insurance coverage or a fiscal
invoice; hospital-api gates the call on the service/tenant configuration, never fires it
unconditionally.

### 2.3 KRA eTIMS transmission (opt-in per tenant/service)

**ADR (2026-07-31):** eTIMS/ETR-compliant invoicing is **opt-in**, not mandatory on every
billable clinical service. Facilities that are VAT/ToT-registered (or otherwise legally required
to issue a fiscal invoice for a given service) enable it per tenant/service in configuration;
treasury-api then follows its existing `pos.sale.finalized`-style flow (submit → `treasury.etims.invoice_transmitted`
→ consumer populates `etims_invoice_number`/`etims_qr_code_url` on the hospital-side record) —
hospital-api never calls the KRA API directly and never stores `ETIMS_*` credentials, mirroring the
eTIMS-ownership ADR already established in `pos-api/docs/integrations.md`.

### 2.4 Taifa Care HMIS — confirmed technical contract (2026-08-29)

SHA's transition to Taifa Care HMIS (announced 2026-06-29, a roughly 90-day provider integration
window) means treasury-api's insurance connector needs a Taifa Care adapter in addition to whatever
NHIF/SHA API it already targets. **This stays a treasury-api enhancement, not hospital-api's job** —
hospital-api only ever calls treasury-api's stable internal contract (`CheckInsuranceEligibility`,
claim submit) regardless of which government system treasury-api talks to underneath.

A set of official DHA UAT API specifications (the "Kenya Digital Superhighway" project, saved in
full at `docs/sha-taifacare-api-specs/`) confirms the actual contract treasury-api's adapter needs to
implement:

- **Auth is two-layer and short-lived.** Basic Auth (credentials issued via the "AfyaLink" developer
  dashboard) against `GET /v1/hie-auth?key={consumer_key}` returns a JWT that expires after **20
  seconds**. treasury-api's adapter must fetch a fresh token immediately before each call, this is
  too short to cache.
- **Client Registry lookup returns encrypted PII.** `GET /v3/client-registry/fetch-client` (by
  identification type/number) returns a patient record with its PII field encrypted, decryptable
  only with a private key whose matching public key is registered on the AfyaLink dashboard. This is
  a real key-custody requirement (the private key must live in treasury-api's secrets store, never
  in hospital-api), not just a bearer-token integration.
- **Facility and practitioner verification are separate, queryable endpoints**:
  `GET /v1/facility-search?facility_code=` returns a facility's approval/operational status and
  license expiry; `GET /v1/practitioner-search?identification_type=&identification_number=` returns
  a practitioner's registration number and active status. These are worth using at onboarding time
  (validate a tenant's facility code, or a doctor's practising license before granting Consultation
  access), not only at claim-submission time.
- **Claims are submitted as a FHIR-style `Bundle`, not a bespoke JSON shape**: `POST
  /v1/shr-med/bundle`, `resourceType: "Bundle"`, `type: "message"`, with `entry` items for
  `Organization` (the facility, an `FID-xx-xxxxxx-x` style ID), `Coverage` (references the `Patient`,
  carries `schemeCategory: "SOCIAL HEALTH AUTHORITY"`), `Patient` (a Client Registry `CR...` ID), and
  `Claim` (`type`/`subType` such as `institutional`/`op`, a `diagnosis` code and display, one or more
  `item`s with a `productOrService` code like `SHA-02-005`, `quantity`, `unitPrice`, and a `total`).
- **Diagnosis coding is ICD-11**, confirmed by the sample claim's `"code": "1A00", "display":
  "Cholera"` (ICD-10's code for cholera is `A00`, not `1A00`). `docs/erd.md`'s
  `diagnosis_catalog_default.code` column, previously described as "ICD-10-ish", should be treated as
  ICD-11 going forward.
- **Claim submission is asynchronous.** The submit call returns a `mediator_id`, not a final
  adjudication, the caller polls `GET /v1/shr-med/claim-status?claim_id=&bundle_id=` for the result.
- **Not confirmed by these specs**: the production base URL (both environments in the source docs
  point at the same `uat.dha.go.ke` host), the exact DHA software-certification workflow (see §2.5),
  rate limits, and whether the `agent` field (examples show `"SAFARICOM-CONSORTIUM-SANDBOX"`) implies
  integration must route through a sanctioned aggregator rather than direct-to-DHA. Confirm the
  access model with DHA before building against a direct-access assumption.

Until the Taifa Care adapter ships, hospital-api should support a manual/CSV claim-upload fallback so
facilities aren't blocked.

### 2.5 DHA Software Certification — a separate, legal gate (not a technical integration)

Distinct from the data-exchange contract in §2.4: under the Digital Health Act 2023 and its 2025
implementing regulations, a healthcare provider may not legally use a digital health solution that
has not been certified by the Digital Health Agency (DHA). This applies to hospital-api and
hospital-ui directly, not only to treasury-api's insurance connector, a tenant cannot go live against
national health systems on an uncertified build regardless of whether the Taifa Care adapter above is
technically complete. Certification also carries its own technical requirements beyond the claims API
(HL7 FHIR R4 conformance, national terminology mapping, a minimum 20-year audit-log retention, a
48-hour DHA breach-notification clock separate from the ODPC's 72-hour one). Full detail, including
which parts of this are confirmed law versus still-unverified vendor commentary, lives in
`docs/compliance-kenya.md` §4. This is a Sprint 12 (`docs/sprints/sprint-12-compliance-hardening.md`)
concern, not a Sprint 5 one.

---

## 2B. KHIS/DHIS2 Aggregate Reporting Integration

**ADR (2026-07-31):** Kenya's Health Information Systems Interoperability Framework (KHISIF)
requires facilities — especially public and donor-funded ones running ART/TB/immunization
programmes — to submit aggregate health indicators to the national KHIS (powered by DHIS2) via the
**ADX (Aggregate Data Exchange)** standard. This is **distinct from SHA/Taifa Care** (which is
insurance-claims reporting, owned by treasury-api) — KHIS reporting is public-health surveillance,
computed from hospital-api's own specialized-programme records (ANC, PNC, ART, TB, Immunization).

**Scope (Phase 2, Sprint 10):** hospital-api computes the required indicator aggregates from its own
data on a schedule and exports them in ADX XML/JSON to the tenant-configured KHIS endpoint (or
provides a downloadable export for facilities that submit manually — many Kenyan facilities still do
this via the KHIS web UI directly). hospital-api does **not** implement the DHIS2 platform itself,
only the export side. Facility Master Facility List (MFL) code is stored as tenant metadata, not a
new schema table.

**External validation (2026-08-29):** DHA's own RMNCAH platform tender (see `docs/compliance-kenya.md`
§8) names DHIS2 as the specific national system a future AI-driven health platform will ingest
facility RMNCAH data from, and lists ANC coverage, skilled birth attendance, immunisation coverage,
and maternal/newborn mortality as the indicators it expects. That is the same target this ADX export
is already aimed at, so the design direction above stands confirmed rather than changed.

**Watch item, not current scope:** the same tender describes a future national platform that writes
data back into facility EMRs (point-of-care consultation transcription and clinical decision-support
flags, only after clinician review). If a national exchange DHIS2-adjacent platform ever calls
hospital-api directly rather than only receiving its exports, that implies an inbound API surface
(accept structured consultation data / clinical flags) in addition to the outbound export above. No
such platform exists yet to integrate against (DHA's own tender is procuring the team that will define
it), so this is a design note for whoever revisits this section once a real conformance suite is
published, not a Sprint 10 task.

---

## 3. Auth Service Integration

Standard SSO pattern, identical to every sibling service:
- JWT validation via `shared-auth-client` (JWKS, RS256, audience `codevertex`).
- JIT user provisioning on first request with a valid token but no local user row
  (`internal/modules/identity`, `EnsureUserFromToken` — self-heals role assignment on every
  request, not just first creation).
- `GET /api/v1/{tenant}/hospital/auth/me` returns hospital-specific role + fine-grained
  `hospital.{module}.{action}` permissions, merged with the global JWT roles (Trinity Layer 3).

**Status (2026-08-01): SHIPPED.** JWKS validator + `RequireAuth` (`internal/app/app.go`), local
RBAC tables (`internal/modules/rbac`, ent schemas `HospitalPermission`/`HospitalRole`/
`RolePermission`/`UserRoleAssignment`), tenant/outlet sync with self-healing UUID-drift repoint
(`internal/modules/tenant/syncer.go`), `auth.user.*`/`auth.outlet.*` NATS event subscribers
(`internal/modules/identity`), and the full middleware chain (`RequireAuth` → `SubscriptionGate`
→ JIT → `TenantV2` → `OutletContext` → route) are all wired in `internal/http/router/router.go`.
auth-service registers `hospital-ui` as an OAuth client and a `"hospital"` outlet use_case
(`ApplicableServices("hospital") -> ["hospital-api"]`) — outlet-sync events now actually reach
this service. Still Sprint 4+ work: no clinical ent schemas yet (Patient/Prescription/etc., see
`docs/migration-pos-pharmacy.md`), so there are no permission-gated domain routes beyond the
placeholder `/ping` yet — only the plumbing those routes will sit behind.

---

## 4. Subscriptions Service Integration

**Status (2026-08-01): SHIPPED.** `service_tag: "hospital"`, three tiers (`AFYA_CLINIC`,
`AFYA_FACILITY`, `AFYA_HOSPITAL`, + one-time perpetual licenses for Clinic/Facility + annual
support plans for all three) seeded in `subscriptions-service/subscriptions-api/cmd/seed/
plans_hospital.go`, matching `CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS PRICING MODEL.md`.
Full cross-service feature matrix per tier (clinical core every tier; `in_house_lab`,
`inpatient_module`, `controlled_substance_register`, `insurance_claims` from Facility;
`theatre_module`, `specialized_programmes`, `multi_branch`, `api_access`, ERP HR/payroll, and
logistics `ambulance_dispatch` at Hospital tier only, per the pricing doc) — see the file header
comment there for the full block-by-block breakdown. hospital-api's own consumer side
(`internal/platform/subscriptions/{client,gate,features}.go`) is wired: mutations-only
enforcement via `SubscriptionGate()` in the router chain, fails open on a subscriptions-api
lookup failure (never lock out an active tenant), matching every other domain service.

---

## 5. Notifications Service Integration

hospital-api never sends SMS/WhatsApp/email directly. It publishes outbox events; notifications-api
subscribes and renders the appropriate template:

| Event | Trigger | Notification |
|---|---|---|
| `hospital.appointment.reminder_due` | Scheduled job, N hours before an appointment | SMS/WhatsApp reminder |
| `hospital.lab_order.resulted` | Lab result entered | "Results ready" SMS to patient |
| `hospital.prescription.ready` | Pharmacy marks a prescription dispensed | "Prescription ready for collection" SMS |

Credit-gated SMS follows the same pattern notifications-api already uses for `isp.*`/`pos.*` events
(per-tenant SMS credit balance, Africa's Talking/Twilio providers).

---

## 6. Migration ADR — Pharmacy/Clinical Logic Currently in pos-api

**See the dedicated plan: [`docs/migration-pos-pharmacy.md`](migration-pos-pharmacy.md).**

**Decision (revised 2026-07-31):** All clinical/pharmacy workflow logic currently living in
`pos-service/pos-api` moves to hospital-api in full, once hospital-api reaches feature parity.
**No backward-compatibility shim is kept** — this is a clean cut, not a dual-write period. **pos-api
keeps no pharmacy logic at all, for any facility size** — a standalone chemist is simply
hospital-api's Pharmacy module used in isolation (see the migration doc § 6), not a separate
pos-api "Dawa" product. This corrects the original 2026-07-31 draft of this ADR, which incorrectly
proposed pos-api retain a standalone chemist product.
