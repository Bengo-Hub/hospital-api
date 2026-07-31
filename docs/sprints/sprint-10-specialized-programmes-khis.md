# Hospital API — Sprint 10: Specialized Care Programmes + KHIS/DHIS2 Aggregate Reporting

**Status:** ⏳ Planned
**Depends on:** Sprint 1-3 (Patient/Visit/Consultation/Lab — programme records reference these)
**Goal:** ANC, PNC, ART, TB, Immunization, Morgue tracking (HosiPoa-parity features) plus aggregate indicator export to Kenya's national KHIS via the ADX standard.

## Ent Schemas to Add

- `anc_record` — `patient_id`, `visit_number`, `risk_flags`.
- `pnc_record` — `patient_id`, `delivery_date`, `follow_up_at`.
- `art_record` — `patient_id`, `regimen`, `adherence_status`.
- `tb_program_record` — `patient_id`, `screening_result`, `treatment_status`.
- `immunization_record` — `patient_id`, `vaccine_code`, `dose_number`, `administered_at`.
- `morgue_record` — `tenant_id`, `body_reference`, `intake_at`, `release_at`, `release_documentation`.
- `khis_export_config` — `tenant_id`, `mfl_code`, `endpoint_url`, `credentials_ref` (encrypted, mirroring `library-api`'s `internal/platform/secrets` pattern — platform-owner or tenant-configurable, not env vars).
- `khis_export_log` — `tenant_id`, `period`, `indicator_set`, `status`, `submitted_at`.

## Endpoints

- One CRUD surface per programme: `/{tenant}/hospital/anc`, `/pnc`, `/art`, `/tb`, `/immunization`, `/morgue`.
- `GET /{tenant}/hospital/khis/indicators?period=` — computed aggregate preview before export.
- `POST /{tenant}/hospital/khis/export` — submit via ADX to the configured KHIS endpoint, or return a downloadable ADX file for manual submission (many Kenyan facilities still submit via the KHIS web UI directly — support both paths).

## Integration Points

- No new S2S dependency beyond hospital-api's own data — KHIS export is a read/aggregate/transmit
  job over data this service already owns.
- Publish `hospital.immunization.recorded` etc. if notifications-api needs them for reminder flows
  (e.g. next-dose-due reminders) — reuse the existing `hospital.appointment.reminder_due` pattern
  rather than inventing a new event shape per programme.

## Definition of Done

- [ ] Each specialized programme's CRUD works and is properly patient-linked.
- [ ] KHIS indicator computation verified against at least the ART/TB/Immunization aggregates (the
      programmes most commonly donor-funded and KHIS-mandatory).
- [ ] ADX export format validated against the KHIS/DHIS2 ADX schema (sample file, not necessarily a
      live submission in this sprint if no sandbox KHIS endpoint is available).
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.

## Next Sprint

Sprint 11 — Subscriptions/licensing (`service_tag: hospital`) + reporting/analytics dashboards.
