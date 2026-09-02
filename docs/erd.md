# Hospital API — Entity Relationship Diagram

**Last updated:** 2026-09-02 (later the same day) — a gap audit (client-facing engineer feedback: IPD
and Theatre "still missing quite a number of sub modules," beds/assets not wired to inventory) added
a further round of **proposed, not-yet-built** additive fields/entities below (bed/ward types,
isolation-precaution flag, structured discharge-summary fields, nursing vitals/ward-round entities, a
theatre staff-assignment entity, an operative-note entity, a PACU-stay entity). One item from that
same round shipped the same day: `equipment_asset_ids` (a JSON array, not the single `asset_id`
originally sketched) on `bed`/`theatre_booking`/`icu_episode` — see `docs/architecture.md`'s
"Biomedical Equipment / Asset Integration" section for the full corrected design and why
`AssetReservation` was deliberately not used. Every addition below is explicitly labeled with its
real status; see also the "Gap audit" sections in `docs/sprints/sprint-6-inpatient.md` /
`sprint-7-theatre-icu.md` for the full reasoning and sourcing behind each one.

**Original entry, still accurate for everything not called out above:** Initial ERD draft written 2026-07-31 alongside the Sprint-0 scaffold.
**As of 2026-09-02, the Patient & Visits, Clinical Workflow, Pharmacy & Dispensing, Billing &
Patient Accounts, Inpatient, and Theatre & Critical Care sections below are real, implemented
`internal/ent/schema/` tables with generated Atlas migrations** (Sprints 1/2/3/4/5-core/6/7 — see
`docs/sprints/`); the Tenant & Outlet Structure / Global Reference Data tables were already
implemented since 2026-08-01. Blood Bank & Transfusion, Ambulance & Emergency Dispatch, and
Specialized Care Programmes remain the **planned** model for Sprint 8 onward — not yet built. A
KenyaEMR technical audit earlier the same day (`docs/kenyaemr-technical-reference.md`) expanded the
specialized-programmes table (VMMC, an OTZ flag on `art_record`, HIV-exposed-infant/PMTCT follow-up,
cervical/prostate cancer screening), added `patient.identification_type`/`identification_number`
(Maisha Number support), and added a `loinc_code` field to the lab-test catalogue — all additive
metadata, carried through into the real Sprint-1/3 schemas as built.

**2026-09-02 — referral/transfer/ambulance-billing gap fix (planned, docs only).** A client-facing
engineer flagged that IPD/OPD referral, transfer, and ambulance workflows were underdeveloped in the
docs relative to what the shipped `Referral`/planned `Ward`/`Bed`/`Admission`/`ambulance_booking`
schemas actually needed. This pass extended (additively) the `referral` row below with inter-facility
and counter-referral fields, added a new planned `patient_transfer` table under Inpatient, and added a
billing linkage to `ambulance_booking`. None of this is built yet; the shipped `Referral` schema is
still exactly the thin shape in `internal/ent/schema/referral.go` today. Full sourcing and design
reasoning: `docs/architecture.md`'s new "Referral, Transfer & Ambulance Billing" section.

---

## Conventions

- **Primary keys:** UUID (`gen_random_uuid()` at the DB or ent default `uuid.New()`), matching every sibling service.
- **Tenant scoping:** business-data tables carry `tenant_id` (UUID, FK to the auth-api tenant, referenced not duplicated). **Reference/catalogue tables do not** — see the Global vs. Tenant-scoped split below (`feedback_shared_core_reference_data.md`). One documented exception (2026-08-30): `hospital_role`'s `tenant_id` is nullable and NULL by default (global), set only on a tenant's own copy-on-write clone or from-scratch custom role — the sanctioned hybrid pattern for genuine per-tenant customization of otherwise-global reference data.
- **Timestamps:** `TIMESTAMPTZ` `created_at`/`updated_at` on every table.
- **Money:** `NUMERIC(18,4)` (never `float`), mirroring `library-api`/`erp-api`.
- **Outlet scoping:** tables that vary per physical location carry a nullable `outlet_id` (branch), following the standard `X-Outlet-ID` optional-filter pattern — absent means tenant-wide.
- **Cross-service references:** `inventory_item_id`, `inventory_lot_id`, `invoice_id`, `insurance_claim_id`, `crm_contact_id` (all nullable UUID FKs, no cross-service foreign-key constraint) plus a snapshot field where display-without-a-live-lookup matters (e.g. `drug_name_snapshot` on `PrescriptionLine`).

---

## Global Reference Data (no `tenant_id` — same for every tenant)

| Table | Key Columns | Description |
|---|---|---|
| `hospital_permission` | `permission_code` (unique), `module`, `action` | Trinity Layer 3 fine-grained permission catalogue (`hospital.{module}.{action}`, ~40 codes) |
| `hospital_role` | `role_code`, `tenant_id` (nullable), `is_system_role`, `cloned_from_role_id` (nullable) | Global roles (`admin`, `doctor`, `nurse`, `pharmacist`, `records_clerk`, `cashier`, `manager`) by default — `tenant_id NULL`. `role_code` is unique per SCOPE, not platform-wide: two partial unique indexes keep global codes (`tenant_id IS NULL`) and per-tenant codes (`tenant_id IS NOT NULL`) in disjoint spaces, so a tenant's copy-on-write clone (2026-08-30, see `rbac.Service.CustomizeRole`) or from-scratch custom role can reuse a global code with no suffix. `cloned_from_role_id` is set only on a clone. |
| `role_permission` | `role_id`, `permission_id` | Role → permission junction |
| `rbac_audit_log` | `tenant_id`, `actor_user_id`, `action`, `target_type`, `target_id`, `before`/`after` (jsonb) | Added 2026-08-30 — minimal audit trail for RBAC mutations only (role assigned/created/customized/permissions-updated, extra role granted/revoked, user status changed). No FK edges (survives its target being hard-deleted). Deliberately narrower than Sprint 12's future platform-wide `audit_log` — see `docs/sprints/sprint-12-compliance-hardening.md`. |
| `hospital_user_outlets` | `tenant_id`, `user_id` (local `hospital_user.id`), `outlet_id`, `is_home_outlet`, `assigned_by`, `assigned_at` | Added 2026-08-30 (follow-up) — per-user outlet/branch assignment, same pattern as pos-api/inventory-api/erp-api's `UserOutlet`/`StaffOutlet`. Enforced by `OutletContextMiddleware`; auto-reconciled from `auth.outlet.*`/`auth.user.*` events. Zero rows for a user = unrestricted (progressive rollout). |
| `diagnosis_catalog_default` | `code` (ICD-11), `name`, `category` | Default diagnosis catalogue seeded once, tenants may add custom entries (those carry `tenant_id`). Confirmed ICD-11 (not ICD-10) by the official DHA claim-submission spec's sample payload, see `docs/sha-taifacare-api-specs/` |
| `lab_test_catalog_default` | `code`, `name`, `specimen_type`, `reference_range`, `loinc_code` (nullable) | Default lab-test catalogue seeded once, tenants may add custom entries. `loinc_code` added 2026-08-29 as an additive metadata field, not a schema overhaul, since Kenya's own national Diagnostics/Patient-Summary FHIR Implementation Guides use LOINC for lab terminology (`docs/kenyaemr-technical-reference.md` §10) |

---

## Tenant & Outlet Structure

| Table | Key Columns | Description |
|---|---|---|
| `hospital_tenant` | `id`, `slug`, `name`, `status` | Minimal synced tenant reference (no branding — fetched from auth-api cache per platform convention) |
| `hospital_user` | `id`, `tenant_id`, `auth_service_user_id`, `status`, `sync_status` | JIT-provisioned local user reference. `id` is a LOCALLY generated UUID, unique only per `(tenant_id, auth_service_user_id)` — fixed 2026-08-30; a prior version used the auth-service user's own UUID as `id` with a platform-wide-unique `auth_service_user_id`, so the same person could only ever hold one row across the entire platform (data corruption for anyone in >1 hospital tenant). Role assignment is via `user_role_assignment` below, not a direct column here. `status` (`active`/`inactive`/`suspended`) is now a real, enforced deactivate/reactivate lifecycle, not write-only. |
| `user_role_assignment` | `tenant_id`, `user_id`, `role_id`, `expires_at` | Tenant-scoped grant of a role (global or a tenant's own clone/custom row). `expires_at` is enforced (2026-08-30, query-time filter) — was previously stored but never checked. A user may hold more than one row (2026-08-30: one "primary" via `SetUserRole`, plus additive "extra" roles via `AssignExtraRole`/`RevokeExtraRole`). |

---

## Patient & Visits (implemented — Sprint 1)

| Table | Key Columns | Description |
|---|---|---|
| `patient` | `id`, `tenant_id`, `mrn` (unique per tenant), `full_name`, `dob`, `sex`, `phone`, `next_of_kin`, `crm_contact_id` (nullable), `client_registry_id` (nullable), `identification_type`, `identification_number` | Patient master record (medical record number) — retained per Kenya DPA's 20-year minimum. `client_registry_id` is the national Client Registry `CR...` identifier returned by DHA's registry lookup (see `docs/sha-taifacare-api-specs/`), stored as a reference so treasury-api's claims never need a second lookup, never treated as a locally-generated ID. `identification_type` is an enum (`national_id`/`passport`/`birth_certificate`/`maisha_number`/`alien_id`) matching the ID types Kenya's own Client Registry accepts (added 2026-08-29, see `docs/compliance-kenya.md` §6) |
| `patient_visit` | `id`, `tenant_id`, `patient_id`, `outlet_id`, `visit_type` (OPD/IPD), `status`, `checked_in_at`, `discharged_at` | One row per encounter, the spine every clinical module hangs off |
| `referral` | `id`, `tenant_id`, `patient_visit_id`, `referred_to`, `referral_type` (new), `reason`, `status`, plus the inter-facility/counter-referral fields listed below (new) | Internal department hand-off (lab/pharmacy/specialist queue) or a genuine inter-facility referral (patient physically leaves for another facility) — see the note directly below |

**2026-09-02 — inter-facility referral fields (planned, additive, not yet built).** A client-facing
gap audit found the shipped `Referral` schema (`referred_to`/`reason`/`status`) is enough for an
internal department hand-off, which the ordinary visit status machine
(`registered→triaged→in_examination→awaiting_lab→...`) already covers well, but not for a genuine
inter-facility referral, where the patient physically leaves for another facility carrying a
referral letter. New fields, all additive, existing rows unaffected:

- `referral_type` (`internal_department` | `inter_facility`) — backfilled on existing rows from the
  current `referred_to` value: `external_facility` maps to `inter_facility`, every other value
  (`lab`/`pharmacy`/`specialist`) maps to `internal_department`. `referred_to` itself is unchanged.
- `referral_summary` (text, nullable) — the referral letter's actual clinical content (presenting
  complaint, diagnosis, treatment already given, reason for referral). Distinct from the existing
  short `reason` field, which stays a one-line reason code/label.
- `receiving_facility_name` (nullable) and `receiving_facility_mfl_code` (nullable) — free text plus
  an optional Kenya Master Facility List code (`docs/compliance-kenya.md` §6). hospital-api has no
  facility directory of its own, so this is not a foreign key to another tenant, only a reference the
  sending facility records for its own letter/register.
- `pre_referral_contact_confirmed` (bool, default `false`) — whether the referring clinician
  confirmed the receiving facility can take the patient before transfer. This is the real first step
  in Kenya's own 2014 national referral guideline (see `docs/architecture.md`'s new referral/transfer
  section for the sourcing), not an invented field.
- `ambulance_booking_id` (nullable UUID, references `ambulance_booking` below) — set when the
  referral required an ambulance leg.
- `counter_referral_received_at` / `counter_referral_summary` / `counter_referral_received_by`
  (all nullable) — feedback from the receiving facility once the patient was actually seen there, the
  standard "counter-referral" concept in referral-systems literature. Kept even though Kenyan studies
  found this step is rarely completed in practice (see `docs/architecture.md`) — the field should
  exist and be usable, adoption is a workflow/training question, not a reason to omit it from the
  schema.
- `status`'s existing `pending`/`acted_on`/`cancelled` values are unchanged. `accepted`/`declined`/
  `completed` are new additive values that only make sense for an `inter_facility` referral — an
  `internal_department` referral keeps using `acted_on` exactly as today.

---

## Clinical Workflow (implemented — Sprints 2/3)

| Table | Key Columns | Description |
|---|---|---|
| `triage_record` | `id`, `patient_visit_id`, `vitals` (BP/temp/pulse/weight), `priority` | Nurse-captured vitals + acuity |
| `examination_record` | `id`, `patient_visit_id`, `clinician_id`, `notes`, `diagnosis_id` | Doctor consultation notes + diagnosis |
| `diagnosis_catalog_entry` | `id`, `tenant_id` (nullable — null = global default), `code`, `name` | Tenant-custom addition to the global diagnosis catalogue |
| `lab_order` | `id`, `patient_visit_id`, `ordered_by`, `status` | Lab request header |
| `lab_order_line` | `id`, `lab_order_id`, `lab_test_id`, `result_value`, `result_at` | One line per requested test |
| `lab_test_catalog_entry` | `id`, `tenant_id` (nullable), `code`, `name` | Tenant-custom addition to the global lab-test catalogue |

---

## Pharmacy & Dispensing (implemented — Sprint 4)

| Table | Key Columns | Description |
|---|---|---|
| `prescription` | `id`, `patient_visit_id`, `prescribed_by`, `status` | Prescription header |
| `prescription_line` | `id`, `prescription_id`, `inventory_item_id`, `dose`, `duration`, `drug_name_snapshot`, `lot_id_snapshot` | One line per drug, drug master fetched live from inventory-api, snapshotted at dispense time |
| `controlled_substance_log` | `id`, `prescription_line_id`, `witnessed_by`, `dispensed_by`, `lot_number`, `expiry_date` | Dual-witness register for scheduled/controlled drugs |
| `drug_interaction_check` | `id`, `prescription_id`, `checked_at`, `findings` (JSON snapshot from inventory-api) | Audit trail of interaction/allergy checks performed at prescribing time |

---

## Billing & Patient Accounts (implemented — Sprint 5 core; see `docs/architecture.md` "Distributed Billing & Patient Accounts")

The **billing ledger**, not the money itself — `invoice_id`/`payment_intent_id` on `billable_charge`
reference treasury-api, which stays the sole owner of every financial document.

| Table | Key Columns | Description |
|---|---|---|
| `billable_item_catalog` | `id`, `tenant_id`, `department`, `code`, `name`, `price` (nullable), `applies_to`, `requires_prepayment`, `collection_mode` | Tenant-configured price list, seeded per facility tier |
| `patient_account` | `id`, `patient_id`, `admission_id` (nullable), `visit_id` (nullable), `status`, `total_charged`, `total_paid`, `balance`, `settlement_required_before`, `next_of_kin_id` (nullable) | One running ledger per patient (spans an admission; per-visit for OPD) |
| `billable_charge` | `id`, `patient_account_id`, `billable_item_id` (nullable), `source_module`, `source_reference_id` (nullable), `amount`, `status`, `treasury_invoice_id` (nullable), `created_by_department`, `paid_at` | One row per charge event, posted by whichever department billed it |
| `patient_next_of_kin` | `id`, `patient_id`, `name`, `phone`, `relationship`, `id_number`, `is_primary` | Who may settle a bill / authorize discharge-release on the patient's behalf; distinct from `patient.next_of_kin` (a free-text chart field, Sprint 1) |
| `walk_in_sale` | `id`, `tenant_id`, `outlet_id`, `prescription_id`, `sale_number`, `patient_name` (nullable), `line_items` (JSON), `amount`, `status`, `payment_method`, `treasury_invoice_id` (nullable), `treasury_payment_intent_id` (nullable), `etims_invoice_number`/`etims_qr_code_url` (nullable), `collected_by` (nullable) | Chemist-tier ledgerless till transaction (added 2026-09-02) — a nil-`patient_id`/`visit_id` prescription's dispense charge, which `patient_account`/`billable_charge` structurally can't hold (both require a real patient account) |

---

## Inpatient (planned — Sprint 6)

| Table | Key Columns | Description |
|---|---|---|
| `ward` | `id`, `tenant_id`, `outlet_id`, `name`, `capacity`, `billable_item_code` (nullable), `is_active` | **Shipped 2026-09-02.** Physical ward definition. `billable_item_code` names the `BillableItemCatalog` code that prices one day in this ward (e.g. `BED_DAY_ICU` vs `BED_DAY_GENERAL`) — a ward-to-ward transfer changes the day-rate automatically because it changes which ward's code applies going forward, see `docs/architecture.md`. Falls back to a tenant-default inpatient day rate if unset. **Proposed, not yet built (2026-09-02 gap audit):** `ward_type` (nullable enum: `general`\|`private`\|`semi_private`\|`isolation`\|`icu`) — gives the UI something to group/filter by and a sensible default suggestion for `billable_item_code` on a new ward; does not replace `billable_item_code`, which stays the actual pricing hook. See `sprint-6-inpatient.md`'s gap audit. |
| `bed` | `id`, `tenant_id`, `ward_id`, `bed_number`, `status`, `equipment_asset_ids` (JSON array) | **Shipped 2026-09-02.** Individual bed within a ward. `equipment_asset_ids` references inventory-api `Asset` IDs (e.g. a bed-mounted monitor) — a list, not a single `asset_id`, since a bed commonly carries more than one piece of fixed equipment; see `docs/architecture.md`'s "Biomedical Equipment / Asset Integration" section. **Proposed, not yet built:** `isolation_precaution` (nullable enum: `contact`\|`droplet`\|`airborne`\|`none`, default `none`) — a per-admission infection-control flag (CDC transmission-based-precaution categories), deliberately modeled per-bed rather than per-ward since isolation is a patient/stay state, not a fixed ward classification. |
| `admission` | `id`, `tenant_id`, `outlet_id`, `patient_visit_id`, `patient_id`, `admission_number`, `ward_id`, `bed_id`, `status` (active/discharged), `admitted_by`, `admitted_at`, `discharged_at`, `discharged_by`, `discharge_summary`, `ward_charge_posted` | **Shipped 2026-09-02.** Admission-to-discharge record. `bed_id`/`ward_id` always reflect the patient's CURRENT location. Every change to it also writes a `patient_transfer` row below, not a silent field update. `ward_charge_posted` guards the discharge-time ward/day-rate charge against double-posting across repeated discharge attempts while a balance is still outstanding. **Proposed, not yet built (2026-09-02 gap audit, sourced from the Joint Commission's mandated discharge-summary components):** `discharge_diagnosis`, `procedures_performed`, `discharge_medications`, `follow_up_instructions` (all text/nullable), `condition_at_discharge` (nullable enum: `recovered`\|`improved`\|`unchanged`\|`deteriorated`\|`deceased`) — additive alongside the existing free-text `discharge_summary`, which is kept as-is for narrative content that doesn't fit the structured fields. See `sprint-6-inpatient.md`'s gap audit. |
| `patient_transfer` (shipped 2026-09-02) | `id`, `tenant_id`, `admission_id`, `transfer_type` (`intra_facility`\|`inter_facility`), `from_ward_id`, `from_bed_id`, `to_ward_id` (nullable), `to_bed_id` (nullable), `receiving_facility_name` (nullable), `referral_id` (nullable), `ambulance_booking_id` (nullable), `reason`, `transferred_by`, `transferred_at` | One row per ward/bed move (intra-facility) or per transfer-out to another facility (inter-facility, closes the admission). See `docs/architecture.md`'s "Referral, Transfer & Ambulance Billing" section for why this is a new table rather than just mutable fields on `admission` — the short version: billing needs to know which ward a patient occupied on which calendar days, and occupancy/audit history needs the same thing, so the move itself has to be a row, not just a field overwrite. |
| `vitals_chart_entry` (**proposed, not yet built**) | `id`, `tenant_id`, `admission_id`, `recorded_by`, `recorded_at`, vitals (BP/temp/pulse/respiratory rate/SpO2), `pain_score`, `notes` | 2026-09-02 gap audit: nursing vitals charting during an inpatient stay, deliberately a NEW entity rather than reusing `TriageRecord` — `TriageRecord` is a one-shot OPD acuity-at-arrival row tied to a visit, never designed to repeat, while inpatient vitals charting is a repeated time series per admission across a multi-day stay. Mirrors `TriageRecord`'s own vitals shape for consistency. See `sprint-6-inpatient.md`'s gap audit. |
| `ward_round_note` (**proposed, not yet built**) | `id`, `tenant_id`, `admission_id`, `clinician_id`, `recorded_at`, `notes`, `diagnosis_id` (nullable) | 2026-09-02 gap audit: a doctor's daily ward-round/progress note, conceptually `ExaminationRecord`'s shape reapplied to an ongoing admission. Kept separate from `vitals_chart_entry` (different author, different RBAC, different clinical content — structured vitals vs. free-text clinical reasoning), not folded into one table. See `sprint-6-inpatient.md`'s gap audit. |
| `visitor_log` (**proposed, lower priority, not yet built**) | `id`, `tenant_id`, `admission_id`, `visitor_name`, `relationship`, `checked_in_at`, `checked_out_at` | 2026-09-02 gap audit: physical visitor check-in/out during a stay — a facilities/access-control concern, distinct from `PatientNextOfKin` (Sprint 5, already sufficient for the billing/discharge-authorization identity question). No research this round found this as a standard baseline "inpatient module" component; flagged as a real but lower-confidence candidate pending actual client demand, not recommended as a default build. |

---

## Theatre & Critical Care (shipped 2026-09-02 — Sprint 7, Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `theatre_booking` | `id`, `tenant_id`, `outlet_id`, `patient_visit_id`, `patient_id`, `theatre_room`, `surgery_type`, `surgeon_id`, `scheduled_at`, `duration_minutes`, `status`, `checklist` (JSON), `fee_amount`, `equipment_asset_ids` (JSON array), `started_at`, `completed_at` | OT scheduling + surgical checklist. `fee_amount` is a snapshot (the seeded `THEATRE_FEE` catalog price is nil — procedure fees vary too widely to price generically). `equipment_asset_ids` (**shipped 2026-09-02**) references inventory-api `Asset` IDs (e.g. an anaesthesia machine) — a list, not a single `asset_id`; see `docs/architecture.md`'s Asset Integration section for why `AssetReservation` was deliberately not used. **Proposed, not yet built (2026-09-02 gap audit):** the `checklist` JSON's default shape should be reworked from the current 5 made-up items into the real WHO Surgical Safety Checklist's 19 items, grouped as `sign_in`/`time_out`/`sign_out` — verbatim item text and full sourcing in `sprint-7-theatre-icu.md`'s gap audit. |
| `icu_episode` | `id`, `tenant_id`, `admission_id`, `bed_id`, `severity_flag`, `monitoring_notes`, `started_by`, `started_at`, `ended_at`, `equipment_asset_ids` (JSON array) | Critical-care episode tracking. No billing fields — an ICU bed's elevated day-rate flows through `ward.billable_item_code` (Sprint 6). `equipment_asset_ids` (**shipped 2026-09-02**) references inventory-api `Asset` IDs (e.g. a ventilator), same shape and reasoning as `theatre_booking` above. |
| `theatre_staff_assignment` (**proposed, not yet built**) | `id`, `tenant_id`, `theatre_booking_id`, `staff_user_id`, `role` (enum: `surgeon`\|`assistant_surgeon`\|`anaesthetist`\|`scrub_nurse`\|`circulating_nurse`\|`other`), `assigned_at` | 2026-09-02 gap audit: multi-role surgical team assignment beyond the single `surgeon_id`. `surgeon_id` is kept as-is for backward compatibility and quick "who's operating" queries; a booking with no assignment rows is treated as having one implied `surgeon` row synthesized from `surgeon_id`. See `sprint-7-theatre-icu.md`'s gap audit for why a table was chosen over named columns (`assistant_surgeon_id`, etc.) — a table scales to any team size/role mix without another migration. |
| `pacu_stay` (**proposed, not yet built**) | `id`, `tenant_id`, `theatre_booking_id`, `bay_label`, `admitted_at`, `discharged_at`, `discharge_disposition` (enum: `to_ward`\|`to_icu`\|`home`\|`deceased`), `monitoring_notes` | 2026-09-02 gap audit: post-anaesthesia care unit (recovery) tracking, deliberately a NEW minimal entity (mirroring `icu_episode`'s own shape) rather than reusing `icu_episode` (most PACU patients are not critically ill; forcing them onto the ICU severity board would be a misleading signal) or a new `theatre_booking` status (the booking tracks the room/procedure turning over, PACU tracks the patient, and the two happen concurrently, not sequentially). If `discharge_disposition` is `to_icu`, a real `icu_episode` starts as it does today. |
| `operative_note` (**proposed, not yet built**) | `id`, `tenant_id`, `theatre_booking_id` (unique), `surgeon_id`, `procedure_performed`, `findings`, `complications`, `estimated_blood_loss_ml`, `implants_used`, `specimens_sent` (bool + text), `post_op_diagnosis`, `authored_by`, `authored_at` | 2026-09-02 gap audit: a structured operative/surgical report (JCAHO/AAAHC-standard components — pre/post-procedure diagnoses, procedure, findings, specimens, complications, blood loss, implants), authored after the procedure completes. Modeled as a one-to-one linked entity rather than new `theatre_booking` fields, since it is a long free-standing clinical document with its own author/timing, not scheduling metadata. |

---

## Blood Bank & Transfusion (planned — Sprint 8, Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `donor_record` | `id`, `tenant_id`, `full_name`, `blood_group`, `last_donation_at`, `eligibility_status` | Blood donor registry |
| `crossmatch_request` | `id`, `patient_visit_id`, `blood_group`, `units_requested`, `status` | Cross-match request for a patient |
| `transfusion_record` | `id`, `crossmatch_request_id`, `inventory_lot_id`, `administered_at`, `administered_by` | Transfusion event; `inventory_lot_id` references the physical blood-bag lot tracked in inventory-api (no local blood-stock table) |

---

## Ambulance & Emergency Dispatch (planned — Sprint 9, thin reference — Afya Hospital tier)

| Table | Key Columns | Description |
|---|---|---|
| `ambulance_booking` | `id`, `patient_visit_id` (nullable — may precede registration), `logistics_task_id`, `pickup_location`, `status`, `fare_amount`, `patient_account_id` (new, nullable), `billable_charge_id` (new, nullable), `referral_id` (new, nullable), `patient_transfer_id` (new, nullable) | Reference row only; dispatch/fleet/pricing all live in logistics-api (`task_type: "ambulance_dispatch"`) |
| `ambulance_membership` | `id`, `tenant_id`, `crm_contact_id`, `plan_type` (individual/family), `expires_at` | Optional recurring membership product; billed via treasury-api, not a new billing engine |

**2026-09-02 — ambulance billing linkage (planned, additive).** `fare_amount` previously had nowhere
to go once known; it was not wired into the Distributed Billing ledger the rest of the platform uses.
New fields: `patient_account_id` (nullable UUID, references `PatientAccount`) is set when the booking
belongs to a patient who already has, or is given, an open ledger, for example an admitted inpatient
being transferred, or an OPD patient whose visit already opened an account. `billable_charge_id`
(nullable UUID) is set once the fare is known and posted as a normal `BillableCharge`
(`department: "ambulance"`, `source_module: "ambulance"`, `source_reference_id: ambulance_booking.id`)
onto that account, exactly like any other department's charge, with no special-cased billing path.
`referral_id` / `patient_transfer_id` (both nullable) link the booking back to whichever inter-facility
referral or transfer triggered it, when applicable. A booking can also carry all four new fields null,
which is the standalone call-out case with no prior clinical record on this platform. See
`docs/architecture.md`'s "Referral, Transfer & Ambulance Billing" section and `docs/integrations.md`
§2A for the full design and the standalone-vs-ledger decision tree.

---

## Specialized Care Programmes (planned — Sprint 10)

| Table | Key Columns | Description |
|---|---|---|
| `anc_record` | `id`, `patient_id`, `visit_number`, `risk_flags` | Antenatal care visit schedule + risk tracking |
| `pnc_record` | `id`, `patient_id`, `delivery_date`, `follow_up_at` | Postnatal mother/newborn follow-up |
| `art_record` | `id`, `patient_id`, `regimen`, `adherence_status`, `is_otz_enrolled` | Antiretroviral therapy tracking (MOH-reporting aligned). `is_otz_enrolled` added 2026-08-29 as an additive flag (not a new table) for Operation Triple Zero, Kenya's real adolescent-HIV adherence/peer-support cohort programme — see `docs/kenyaemr-technical-reference.md` §4 |
| `tb_program_record` | `id`, `patient_id`, `screening_result`, `treatment_status` | TB screening/treatment/follow-up. National case-based reporting for TB runs through a separate system (TIBU), distinct from the general KHIS/ADX export below — see `sprint-10-specialized-programmes-khis.md` |
| `immunization_record` | `id`, `patient_id`, `vaccine_code`, `dose_number`, `administered_at` | Vaccine schedule + coverage reporting |
| `vmmc_record` | `id`, `patient_id`, `procedure_date`, `complications`, `follow_up_at` | Voluntary Medical Male Circumcision — procedure and follow-up record (new 2026-08-29, mirrors the minimal-table shape every other programme already uses) |
| `hei_record` | `id`, `patient_id`, `mother_patient_id` (nullable), `pcr_test_schedule` (JSON — 6-8 week / 6 month / final test dates, the standard EID cascade), `final_status` | HIV-Exposed Infant follow-up (the infant side of PMTCT), linked to the mother's `anc_record`/`art_record` where known (new 2026-08-29) |
| `cancer_screening_record` | `id`, `patient_id`, `screening_type` (cervical/prostate), `result`, `follow_up_at` | Cancer screening event + follow-up (new 2026-08-29 — KenyaEMR's own module covers both cervical and prostate, not cervical-only) |
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
