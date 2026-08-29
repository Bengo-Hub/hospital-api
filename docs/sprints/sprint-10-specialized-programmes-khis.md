# Hospital API — Sprint 10: Specialized Care Programmes + KHIS/DHIS2 Aggregate Reporting

**Status:** ⏳ Planned
**Depends on:** Sprint 1-3 (Patient/Visit/Consultation/Lab — programme records reference these)
**Goal:** ANC, PNC, ART, TB, Immunization, VMMC, PMTCT/EID, cancer screening, Morgue tracking
(HosiPoa-parity features) plus aggregate indicator export to Kenya's national KHIS via the ADX
standard.

**Programme list expanded 2026-08-29** after a KenyaEMR technical audit
(`docs/kenyaemr-technical-reference.md` §4) found VMMC, an adolescent-HIV cohort programme (OTZ),
PMTCT/HIV-Exposed-Infant follow-up, and cervical + prostate cancer screening are real, distinct
modules in Kenya's dominant clinical EMR, not an internally-invented wishlist.

## Ent Schemas to Add

- `anc_record` — `patient_id`, `visit_number`, `risk_flags`.
- `pnc_record` — `patient_id`, `delivery_date`, `follow_up_at`.
- `art_record` — `patient_id`, `regimen`, `adherence_status`, `is_otz_enrolled` (additive flag for
  Operation Triple Zero, the adolescent-adherence cohort, not a new table).
- `tb_program_record` — `patient_id`, `screening_result`, `treatment_status`.
- `immunization_record` — `patient_id`, `vaccine_code`, `dose_number`, `administered_at`.
- `vmmc_record` — `patient_id`, `procedure_date`, `complications`, `follow_up_at` (new).
- `hei_record` — `patient_id`, `mother_patient_id` (nullable), `pcr_test_schedule` (JSON — the
  standard 6-8 week / 6 month / final EID test dates), `final_status` (new — HIV-Exposed Infant
  follow-up, the infant side of PMTCT).
- `cancer_screening_record` — `patient_id`, `screening_type` (cervical/prostate), `result`,
  `follow_up_at` (new).
- `morgue_record` — `tenant_id`, `body_reference`, `intake_at`, `release_at`, `release_documentation`.
- `khis_export_config` — `tenant_id`, `mfl_code`, `endpoint_url`, `credentials_ref` (encrypted, mirroring `library-api`'s `internal/platform/secrets` pattern — platform-owner or tenant-configurable, not env vars).
- `khis_export_log` — `tenant_id`, `period`, `indicator_set`, `status`, `submitted_at`.

## Endpoints

- One CRUD surface per programme: `/{tenant}/hospital/anc`, `/pnc`, `/art`, `/tb`, `/immunization`, `/vmmc`, `/hei`, `/cancer-screening`, `/morgue`.
- `GET /{tenant}/hospital/khis/indicators?period=` — computed aggregate preview before export.
- `POST /{tenant}/hospital/khis/export` — submit via ADX to the configured KHIS endpoint, or return a downloadable ADX file for manual submission (many Kenyan facilities still submit via the KHIS web UI directly — support both paths).

## Compliance context: the real national register/reporting targets

**Corrected MOH register mapping (2026-08-29)** — an earlier internal assumption had HTS mapped to
the wrong form. The real mapping, from NASCOP/MOH/NPHI sourcing (full detail
`docs/kenyaemr-technical-reference.md` §4):

| Register / Form | Programme |
|---|---|
| MOH 731 | ART (comprehensive HIV/AIDS report) |
| MOH 362 | HTS Lab, Referral & Linkage Register — the real HIV-testing register |
| MOH 705A / 705B | OPD summary (under-5 / over-5) — not a specialized-programme register |
| MOH 711 | RMNCH summary (ANC/PNC/Immunization roll-up) |
| MOH 502 | IDSR case-based reporting for notifiable diseases — a distinct family from the above |

**TB has its own separate national system, not just KHIS/ADX.** Kenya runs a dedicated, TB-only
case-based surveillance platform, TIBU, that TB coordinators feed monthly/quarterly, which only rolls
up into KHIS2 as a quarterly aggregate. `tb_program_record`'s export design should plan for two
distinct downstream targets eventually — the routine ADX aggregate this sprint builds, and,
separately, a TIBU-shaped case-based export — rather than assuming ADX alone covers TB reporting the
way it does for the other programmes.

**Automating this export has a proven, citable accuracy case.** A published field test of Kenya's
own "Automated Indicator Reporting" mechanism (EMR-generated ADX submissions to DHIS2) found
automated MOH-731 data was 100% complete and accurate, versus 89% complete / 71% accurate for
manually entered data over the same period. Worth using this as the value-proposition anchor for this
sprint's dashboards and export tooling, not just a compliance checkbox.

**Design option worth flagging, not committing to this sprint**: Kenya's own reporting stack for
this same HIV cascade data has moved beyond aggregate batch export, to a real-time Kafka-based
event-streaming pipeline for milestones like PrEP uptake and EID testing stages. This sprint's ADX
batch design is still the right starting point, but should not be assumed to be the long-term ceiling
if this product later needs to compete on reporting timeliness.

## Integration Points

- No new S2S dependency beyond hospital-api's own data — KHIS export is a read/aggregate/transmit
  job over data this service already owns.
- Publish `hospital.immunization.recorded` etc. if notifications-api needs them for reminder flows
  (e.g. next-dose-due reminders) — reuse the existing `hospital.appointment.reminder_due` pattern
  rather than inventing a new event shape per programme.

## Definition of Done

- [ ] Each specialized programme's CRUD works and is properly patient-linked, including the three
      new programmes (VMMC, HEI/PMTCT, cancer screening) and the ART `is_otz_enrolled` flag.
- [ ] KHIS indicator computation verified against at least the ART/TB/Immunization aggregates (the
      programmes most commonly donor-funded and KHIS-mandatory).
- [ ] ADX export format validated against the KHIS/DHIS2 ADX schema (sample file, not necessarily a
      live submission in this sprint if no sandbox KHIS endpoint is available).
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.

## Next Sprint

Sprint 11 — Subscriptions/licensing (`service_tag: hospital`) + reporting/analytics dashboards.
