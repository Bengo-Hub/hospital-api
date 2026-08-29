# Hospital API — Sprint 12: Compliance Hardening (Kenya Data Protection Act)

**Status:** ⏳ Planned
**Depends on:** all domain sprints (audit trail must cover every mutating action across every module)
**Goal:** Consent capture, audit trail, and retention policy for sensitive health data, aligned to Kenya's Data Protection Act and the ODPC's Certificate of Data Handler/Processor requirement.

## Context

Research this round confirmed: health data is legally classified as "sensitive personal data" in
Kenya, requiring a valid Certificate of Data Handler/Processor for facility registration (effective
2025), a 20-year minimum retention period, and consent before collecting/sharing patient data unless
a legal exemption applies.

A later research pass (2026-08-29, see `docs/compliance-kenya.md`) found a second, separate, and
legally binding obligation this sprint had not yet covered: under the Digital Health Act 2023 and its
2025 implementing regulations, a healthcare provider may not use a digital health solution unless the
Digital Health Agency (DHA) has certified it (Reg 37(1)). This is not the ODPC Certificate of Data
Handler/Processor above, it is a separate certification of the software itself, with its own
technical requirements: HL7 FHIR R4 conformance, national terminology mapping (the confirmed claims
API uses ICD-11, see `docs/sha-taifacare-api-specs/`), a 20-year minimum audit-log retention
(Reg 12(c)(ii), consistent with the DPA retention task below), and a 48-hour DHA breach-notification
clock that runs alongside, not instead of, the ODPC's 72-hour one. Certification fees range KES
10,000 to 500,000 depending on solution type (Reg 38(3)).

## Ent Schemas to Add

- `consent_record` — `patient_id`, `consent_type` (treatment/data-sharing/research), `granted_at`,
  `revoked_at`, `granted_by`.
- `audit_log` — `tenant_id`, `actor_user_id`, `action`, `entity_type`, `entity_id`, `metadata` (JSON),
  `created_at` — mirror `library-api`'s existing `auditlog` ent schema shape rather than inventing a
  new one.

A third research pass (2026-08-29, `docs/compliance-kenya.md` §9-10) found the certification
requirement above sits inside a broader legally-defined architecture: an Enterprise Service Bus with
its own onboarding process, fee schedule, and a concrete Shared Health Record update obligation. It
also found the regulation numbers already cited in this document and in `docs/compliance-kenya.md`
§4 likely belong to a different instrument than the newly-read draft regulation text, do not assume
Reg numbers are interchangeable across the Digital Health Act's several 2024/2025 regulations without
naming which instrument a citation belongs to.

## Tasks

- [ ] Audit-log middleware wraps every mutating endpoint across every module (not opt-in per
      handler — a cross-cutting concern, applied once at the router level).
- [ ] Consent capture at patient registration (Sprint 1) and before any data-sharing action
      (referral to another facility, insurance claim submission).
- [ ] Retention policy: patient records are never hard-deleted; a tenant-offboarding flow archives
      rather than purges, honoring the 20-year minimum.
- [ ] Data export/delete-request endpoints for patient-initiated DPA requests (export always
      possible; delete is a legal-review gate given the retention requirement, not a raw DELETE).
- [ ] Document the facility's Certificate of Data Handler/Processor number as tenant metadata
      (informational, not enforced by code — a legal/business requirement, not a technical gate).
- [ ] Document the facility's DHA certification status/reference number as tenant metadata,
      alongside (not instead of) the ODPC Certificate of Data Handler/Processor number above, these
      are two different regulators and two different numbers, see `docs/compliance-kenya.md` § 1.
- [ ] `audit_log` retention honors the DHA regulation's 20-year minimum (Reg 12(c)(ii)), not just the
      DPA's own, less specific guidance, since a DHA-connected system is held to the stricter figure.
- [ ] Breach-notification runbook covers both clocks: 48 hours to the DHA CEO (Reg 11(2)(a)) and 72
      hours to the ODPC (DPA), since a single incident can trigger both obligations at once.
- [ ] Diagnosis catalogue (`diagnosis_catalog_default`, see `docs/erd.md`) is seeded with ICD-11
      codes, matching the coding scheme the confirmed DHA claims API actually expects.
- [ ] Shared Health Record update obligation tracked as a named requirement, not a vague "FHIR
      someday" note: once DHA certification is pursued, every client encounter must push an update
      to the national SHR within 24 hours (7-day grace only if the solution is offline), mapping onto
      the Kenya Patient Summary FHIR profiles (Patient, Condition, MedicationStatement, Encounter,
      Observation, Immunization, Claim, ClaimResponse, Coverage, ExplanationOfBenefit,
      MedicationDispense) — see `docs/integrations.md` §2C.
- [ ] ESB onboarding mechanics documented as a tenant-onboarding checklist item (Form 1 application,
      DHA's 14-day review commitment, the renewable 1-year enterprise-user licence, and the
      per-facility-level fee schedule) once the certification instrument's final numbering is
      confirmed — see `docs/compliance-kenya.md` §9.
- [ ] Patient registration/consent supports `maisha_number` as a distinct identification type
      alongside `national_id`/`passport`/`birth_certificate`/`alien_id` (Sprint 1 schema, `docs/erd.md`),
      matching the ID types Kenya's own Client Registry accepts.
- [ ] hospital-api's/hospital-ui's own eventual DHA certification readiness references the Kenya Core
      and Kenya Patient Summary FHIR Implementation Guides (`docs/compliance-kenya.md` §10) as the
      concrete conformance target, not a generic "HL7 FHIR" aspiration.

## Definition of Done

- [ ] Every mutating endpoint produces an audit-log row.
- [ ] Consent is required and recorded before a referral or insurance-claim submission proceeds.
- [ ] No hard-delete code path exists for `Patient`/`PatientVisit`/clinical records.
- [ ] Both the ODPC Certificate of Data Handler/Processor number and the DHA certification reference
      are stored as tenant metadata, distinctly.
- [ ] `go build`/`go vet` clean.

## Next Sprint

Sprint 13 — Launch + decisive pos-api pharmacy decommission.
