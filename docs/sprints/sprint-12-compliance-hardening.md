# Hospital API — Sprint 12: Compliance Hardening (Kenya Data Protection Act)

**Status:** ⏳ Planned
**Depends on:** all domain sprints (audit trail must cover every mutating action across every module)
**Goal:** Consent capture, audit trail, and retention policy for sensitive health data, aligned to Kenya's Data Protection Act and the ODPC's Certificate of Data Handler/Processor requirement.

## Context

Research this round confirmed: health data is legally classified as "sensitive personal data" in
Kenya, requiring a valid Certificate of Data Handler/Processor for facility registration (effective
2025), a 20-year minimum retention period, and consent before collecting/sharing patient data unless
a legal exemption applies.

## Ent Schemas to Add

- `consent_record` — `patient_id`, `consent_type` (treatment/data-sharing/research), `granted_at`,
  `revoked_at`, `granted_by`.
- `audit_log` — `tenant_id`, `actor_user_id`, `action`, `entity_type`, `entity_id`, `metadata` (JSON),
  `created_at` — mirror `library-api`'s existing `auditlog` ent schema shape rather than inventing a
  new one.

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

## Definition of Done

- [ ] Every mutating endpoint produces an audit-log row.
- [ ] Consent is required and recorded before a referral or insurance-claim submission proceeds.
- [ ] No hard-delete code path exists for `Patient`/`PatientVisit`/clinical records.
- [ ] `go build`/`go vet` clean.

## Next Sprint

Sprint 13 — Launch + decisive pos-api pharmacy decommission.
