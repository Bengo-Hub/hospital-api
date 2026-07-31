# Hospital API — Sprint 7: Theatre/OT Scheduling & ICU/Critical-Care Monitoring

**Status:** ⏳ Planned
**Depends on:** Sprint 6 (Inpatient — theatre bookings and ICU episodes both hang off an admission)
**Goal:** Surgery scheduling with an OT checklist and staff/case-load assignment; ICU bed-level monitoring flags. Afya Hospital tier only.

## Context

Research this round (see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md` Round 2)
found that Operating Room management in general HIS products centers on surgery scheduling
(date/time/type/checklist) plus staff-shift assignment aligned to case demand — conceptually the
same "resource booking + staff assignment" shape as pos-api's existing Facility/FacilityBooking
hospitality pattern. Reuse that *shape* for the API design (booking header + resource + status
lifecycle), but this is clinically owned by hospital-api, not pos-api.

## Ent Schemas to Add

- `theatre_booking` — `patient_visit_id`, `theatre_room`, `surgery_type`, `scheduled_at`, `status`, `checklist_json` (a JSON field for the pre-op checklist — normalize via JSON rather than a rigid new table per checklist item, per the "additive metadata over new schema" preference).
- `icu_episode` — `admission_id`, `bed_id`, `severity_flag`, `monitoring_notes`, `started_at`, `ended_at`.

## Endpoints

- `POST /{tenant}/hospital/theatre-bookings` — schedule a surgery.
- `GET /{tenant}/hospital/theatre-bookings?date=` — theatre-room availability/schedule view.
- `POST /{tenant}/hospital/theatre-bookings/{id}/checklist` — update the pre-op checklist.
- `POST /{tenant}/hospital/icu-episodes` — start an ICU episode for an admitted patient.
- `PATCH /{tenant}/hospital/icu-episodes/{id}` — update severity/monitoring notes.

## Integration Points

- None new — both features consume `admission`/`bed` from Sprint 6.

## Definition of Done

- [ ] Theatre scheduling with conflict detection (no double-booking a theatre room/time slot).
- [ ] ICU episode lifecycle tied correctly to admission/discharge.
- [ ] Atlas migration generated and committed. `go build`/`go vet` clean.

## Next Sprint

Sprint 8 — Blood Bank & Transfusion.
