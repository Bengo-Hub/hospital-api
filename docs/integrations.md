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

### 2.4 Future: Taifa Care HMIS

SHA's mandatory transition to Taifa Care HMIS (2026-06-29, 90-day provider integration deadline)
means treasury-api's insurance connector needs a Taifa Care HMIS adapter in addition to whatever
NHIF/SHA API it already targets. **This is a treasury-api enhancement, not hospital-api's job** —
hospital-api only ever calls treasury-api's stable internal contract (`CheckInsuranceEligibility`,
claim submit) regardless of which government system treasury-api talks to underneath. Until the
Taifa Care adapter ships, hospital-api should support a manual/CSV claim-upload fallback so
facilities aren't blocked.

---

## 3. Auth Service Integration

Standard SSO pattern, identical to every sibling service:
- JWT validation via `shared-auth-client` (JWKS, RS256, audience `codevertex`).
- JIT user provisioning on first request with a valid token but no local user row.
- `GET /api/v1/{tenant}/hospital/auth/me` (planned) returns hospital-specific role + fine-grained
  `hospital.{module}.{action}` permissions, merged with the global JWT roles (Trinity Layer 3).

**Status:** JWKS validator + `RequireAuth` middleware are wired today (`internal/app/app.go`); the
`/auth/me` endpoint and local RBAC tables are not implemented yet (Sprint 1).

---

## 4. Subscriptions Service Integration

**Planned `service_tag`:** `hospital`, three tiers (`AFYA_CLINIC`, `AFYA_FACILITY`, `AFYA_HOSPITAL`)
matching `d:\Projects\Codevertex\CODEVERTEX AFRICA HOSPITAL MANAGEMENT SYSTEMS PRICING MODEL.md`.
Feature gates to register: `inpatient_module` (Clinic add-on / included from Facility up),
`in_house_lab`, `insurance_claims`, `multi_branch`, `specialized_programmes`, `api_access`.
Mutations-only subscription enforcement, matching every other domain service (auth-api and
subscriptions-api themselves are exempt).

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

**Decision (2026-07-31):** All clinical/pharmacy workflow logic currently living in
`pos-service/pos-api` moves to hospital-api in full, once hospital-api reaches feature parity.
**No backward-compatibility shim is kept** — per the platform's own migration-notes convention
(`shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md` § Migration Notes), this is a clean cut, not a
dual-write period.

**To be removed from pos-api once hospital-api ships the equivalent:**
- Ent schemas: `Patient`, `PatientVisit`, `TriageRecord`, `ExaminationRecord`, `LabOrder`,
  `LabOrderLine`, `LabTest`, `DiagnosisCatalog`, `Prescription`, `PrescriptionLine`,
  `ControlledSubstanceLog`, `DrugInteractionCheck`.
- Handlers: `pharmacy.go`, `pharmacy_checkout.go`, `pharmacy_controlled.go`, `clinical.go`,
  `clinical_records.go`, `clinical_triage.go`, `clinical_examination.go`, `clinical_lab.go`,
  `clinical_bills.go`, `clinical_catalog.go`, `clinical_settings.go`, `report_pdf_pharmacy.go`.
- Modules: `internal/modules/printing/dispensing_label.go`, `cmd/seed/seed_clinical_catalogs.go`.
- Migrations: `20260520213213_sprint8_9_pharmacy_service.sql`,
  `20260525014738_add_pharmacy_regulatory_fields.sql`, `20260721220942_prescription_metadata.sql`,
  `20260723141538_controlled_substance_log_lot_fields.sql`,
  `20260725054849_opd_clinical_workflow.sql`,
  `20260727230708_lab_test_catalog_diagnoses_workflow_mode.sql`.
- Frontend: `pos-ui` routes/components under `pharmacy/**`, `patients`, `examination`,
  `components/clinical/**`, `PharmacyTerminalView.tsx`, `PharmacyWorkflowTab.tsx`,
  `hooks/usePharmacy.ts`, `hooks/useClinical.ts`, `lib/api/{pharmacy,clinical}.ts`.
- Docs: `pos-service/pos-api/docs/sprints/sprint-8-pharmacy-module.md`,
  `sprint-9-service-module.md` (superseded by this service's sprint docs).

**pos-api keeps:** the standalone "Codevertex Dawa" retail-pharmacy/chemist product — OTC till
sale of drug SKUs sourced from inventory-api, no clinical workflow, no prescriptions. A tenant uses
either pos-api Dawa (chemist) or hospital-api's pharmacy module (hospital/clinic), never both for
the same outlet.

**Execution note:** this migration is **not part of the current round** (docs + pricing + scaffold
only). It happens once hospital-api's pharmacy module (Sprint 4) reaches feature parity with what
pos-api already has in production.
