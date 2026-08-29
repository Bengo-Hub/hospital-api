# Hospital API — Entity Relationship Diagram

**Last updated:** 2026-07-31 — Initial ERD draft written alongside the Sprint-0 scaffold. No ent
schemas exist yet (`internal/ent/schema/` is empty) — the tables below are the planned model for
Sprint 1 onward, not yet implemented.

---

## Conventions

- **Primary keys:** UUID (`gen_random_uuid()` at the DB or ent default `uuid.New()`), matching every sibling service.
- **Tenant scoping:** business-data tables carry `tenant_id` (UUID, FK to the auth-api tenant, referenced not duplicated). **Reference/catalogue tables do not** — see the Global vs. Tenant-scoped split below (`feedback_shared_core_reference_data.md`).
- **Timestamps:** `TIMESTAMPTZ` `created_at`/`updated_at` on every table.
- **Money:** `NUMERIC(18,4)` (never `float`), mirroring `library-api`/`erp-api`.
- **Outlet scoping:** tables that vary per physical location carry a nullable `outlet_id` (branch), following the standard `X-Outlet-ID` optional-filter pattern — absent means tenant-wide.
- **Cross-service references:** `inventory_item_id`, `inventory_lot_id`, `invoice_id`, `insurance_claim_id`, `crm_contact_id` (all nullable UUID FKs, no cross-service foreign-key constraint) plus a snapshot field where display-without-a-live-lookup matters (e.g. `drug_name_snapshot` on `PrescriptionLine`).

---

## Global Reference Data (no `tenant_id` — same for every tenant)

| Table | Key Columns | Description |
|---|---|---|
| `hospital_permission` | `permission_code` (unique), `module`, `action` | Trinity Layer 3 fine-grained permission catalogue (`hospital.{module}.{action}`) |
| `hospital_role` | `role_code` (unique), `is_system_role` | Global roles (`hospital_admin`, `doctor`, `lab_technician`, `pharmacist`, `cashier`, `receptionist`) — same permissions for every tenant |
| `role_permission` | `role_id`, `permission_id` | Role → permission junction |
| `diagnosis_catalog_default` | `code` (ICD-11), `name`, `category` | Default diagnosis catalogue seeded once, tenants may add custom entries (those carry `tenant_id`). Confirmed ICD-11 (not ICD-10) by the official DHA claim-submission spec's sample payload, see `docs/sha-taifacare-api-specs/` |
| `lab_test_catalog_default` | `code`, `name`, `specimen_type`, `reference_range` | Default lab-test catalogue seeded once, tenants may add custom entries |

---

## Tenant & Outlet Structure

| Table | Key Columns | Description |
|---|---|---|
| `hospital_tenant` | `id`, `slug`, `name`, `status` | Minimal synced tenant reference (no branding — fetched from auth-api cache per platform convention) |
| `hospital_user` | `id`, `auth_service_user_id`, `role_id`, `sync_status` | JIT-provisioned local user reference |
| `user_role_assignment` | `tenant_id`, `user_id`, `role_id`, `expires_at` | Tenant-scoped grant of a global role |

---

## Patient & Visits

| Table | Key Columns | Description |
|---|---|---|
| `patient` | `id`, `tenant_id`, `mrn` (unique per tenant), `full_name`, `dob`, `sex`, `phone`, `next_of_kin`, `crm_contact_id` (nullable), `client_registry_id` (nullable) | Patient master record (medical record number) — retained per Kenya DPA's 20-year minimum. `client_registry_id` is the national Client Registry `CR...` identifier returned by DHA's registry lookup (see `docs/sha-taifacare-api-specs/`), stored as a reference so treasury-api's claims never need a second lookup, never treated as a locally-generated ID |
| `patient_visit` | `id`, `tenant_id`, `patient_id`, `outlet_id`, `visit_type` (OPD/IPD), `status`, `checked_in_at`, `discharged_at` | One row per encounter, the spine every clinical module hangs off |
| `referral` | `id`, `tenant_id`, `patient_visit_id`, `referred_to`, `reason`, `status` | Inter-facility or inter-department referral |

---

## Clinical Workflow

| Table | Key Columns | Description |
|---|---|---|
| `triage_record` | `id`, `patient_visit_id`, `vitals` (BP/temp/pulse/weight), `priority` | Nurse-captured vitals + acuity |
| `examination_record` | `id`, `patient_visit_id`, `clinician_id`, `notes`, `diagnosis_id` | Doctor consultation notes + diagnosis |
| `diagnosis_catalog_entry` | `id`, `tenant_id` (nullable — null = global default), `code`, `name` | Tenant-custom addition to the global diagnosis catalogue |
| `lab_order` | `id`, `patient_visit_id`, `ordered_by`, `status` | Lab request header |
| `lab_order_line` | `id`, `lab_order_id`, `lab_test_id`, `result_value`, `result_at` | One line per requested test |
| `lab_test_catalog_entry` | `id`, `tenant_id` (nullable), `code`, `name` | Tenant-custom addition to the global lab-test catalogue |

---

## Pharmacy & Dispensing

| Table | Key Columns | Description |
|---|---|---|
| `prescription` | `id`, `patient_visit_id`, `prescribed_by`, `status` | Prescription header |
| `prescription_line` | `id`, `prescription_id`, `inventory_item_id`, `dose`, `duration`, `drug_name_snapshot`, `lot_id_snapshot` | One line per drug, drug master fetched live from inventory-api, snapshotted at dispense time |
| `controlled_substance_log` | `id`, `prescription_line_id`, `witnessed_by`, `dispensed_by`, `lot_number`, `expiry_date` | Dual-witness register for scheduled/controlled drugs |
| `drug_interaction_check` | `id`, `prescription_id`, `checked_at`, `findings` (JSON snapshot from inventory-api) | Audit trail of interaction/allergy checks performed at prescribing time |

---

## Billing & Patient Accounts (Sprint 5 — see `docs/architecture.md` "Distributed Billing & Patient Accounts")

The **billing ledger**, not the money itself — `invoice_id`/`payment_intent_id` on `billable_charge`
reference treasury-api, which stays the sole owner of every financial document.

| Table | Key Columns | Description |
|---|---|---|
| `billable_item_catalog` | `id`, `tenant_id`, `department`, `code`, `name`, `price` (nullable), `applies_to`, `requires_prepayment`, `collection_mode` | Tenant-configured price list, seeded per facility tier |
| `patient_account` | `id`, `patient_id`, `admission_id` (nullable), `visit_id` (nullable), `status`, `total_charged`, `total_paid`, `balance`, `settlement_required_before`, `next_of_kin_id` (nullable) | One running ledger per patient (spans an admission; per-visit for OPD) |
| `billable_charge` | `id`, `patient_account_id`, `billable_item_id` (nullable), `source_module`, `source_reference_id` (nullable), `amount`, `status`, `treasury_invoice_id` (nullable), `created_by_department`, `paid_at` | One row per charge event, posted by whichever department billed it |
| `patient_next_of_kin` | `id`, `patient_id`, `name`, `phone`, `relationship`, `id_number`, `is_primary` | Who may settle a bill / authorize discharge-release on the patient's behalf; distinct from `patient.next_of_kin` (a free-text chart field, Sprint 1) |

---

## Inpatient

| Table | Key Columns | Description |
|---|---|---|
| `ward` | `id`, `tenant_id`, `outlet_id`, `name`, `capacity` | Physical ward definition |
| `bed` | `id`, `ward_id`, `bed_number`, `status` | Individual bed within a ward |
| `admission` | `id`, `patient_visit_id`, `bed_id`, `admitted_at`, `discharged_at`, `discharge_summary` | Admission-to-discharge record |

---

## Theatre & Critical Care (Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `theatre_booking` | `id`, `patient_visit_id`, `theatre_room`, `surgery_type`, `scheduled_at`, `status`, `checklist_json` | OT scheduling + surgical checklist |
| `icu_episode` | `id`, `admission_id`, `bed_id`, `severity_flag`, `monitoring_notes`, `started_at`, `ended_at` | Critical-care episode tracking |

---

## Blood Bank & Transfusion (Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `donor_record` | `id`, `tenant_id`, `full_name`, `blood_group`, `last_donation_at`, `eligibility_status` | Blood donor registry |
| `crossmatch_request` | `id`, `patient_visit_id`, `blood_group`, `units_requested`, `status` | Cross-match request for a patient |
| `transfusion_record` | `id`, `crossmatch_request_id`, `inventory_lot_id`, `administered_at`, `administered_by` | Transfusion event; `inventory_lot_id` references the physical blood-bag lot tracked in inventory-api (no local blood-stock table) |

---

## Ambulance & Emergency Dispatch (thin reference — Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `ambulance_booking` | `id`, `patient_visit_id` (nullable — may precede registration), `logistics_task_id`, `pickup_location`, `status`, `fare_amount` | Reference row only; dispatch/fleet/pricing all live in logistics-api (`task_type: "ambulance_dispatch"`) |
| `ambulance_membership` | `id`, `tenant_id`, `crm_contact_id`, `plan_type` (individual/family), `expires_at` | Optional recurring membership product; billed via treasury-api, not a new billing engine |

---

## Specialized Care Programmes

| Table | Key Columns | Description |
|---|---|---|
| `anc_record` | `id`, `patient_id`, `visit_number`, `risk_flags` | Antenatal care visit schedule + risk tracking |
| `pnc_record` | `id`, `patient_id`, `delivery_date`, `follow_up_at` | Postnatal mother/newborn follow-up |
| `art_record` | `id`, `patient_id`, `regimen`, `adherence_status` | Antiretroviral therapy tracking (MOH-reporting aligned) |
| `tb_program_record` | `id`, `patient_id`, `screening_result`, `treatment_status` | TB screening/treatment/follow-up |
| `immunization_record` | `id`, `patient_id`, `vaccine_code`, `dose_number`, `administered_at` | Vaccine schedule + coverage reporting |
| `morgue_record` | `id`, `tenant_id`, `body_reference`, `intake_at`, `release_at`, `release_documentation` | Body registration, storage, and release |

---

## Cross-Service References (no local tables)

These are referenced by ID only — never modeled locally in hospital-api (see `architecture.md`
Data Authority table and `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`):

| Reference field | Owner service |
|---|---|
| `inventory_item_id`, `inventory_lot_id` | inventory-api |
| `asset_id` (biomedical equipment, beds, ambulance capital assets — already modeled as `Asset`/`AssetMaintenance`) | inventory-api |
| `invoice_id`, `payment_intent_id`, `insurance_claim_id` | treasury-api |
| `logistics_task_id` (ambulance dispatch task) | logistics-api |
| `auth_service_user_id`, `tenant_id` | auth-api |
| `crm_contact_id` (optional, billing-contact linkage) | marketflow-api |
