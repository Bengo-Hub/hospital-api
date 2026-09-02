# Hospital API — Sprint 7: Theatre/OT Scheduling & ICU/Critical-Care Monitoring

**Status:** ✅ Shipped 2026-09-02 (`hospital-api@845b82b` — see `.claude/plans/hospital-sprint7-theatre-icu-2026-09-02.md` for the full execution record). `go build`/`go vet`/`go test` green, Atlas migration generated + applied to the local dev DB, real integration test (`internal/integration/theatre_icu_golden_path_test.go`).
**Depends on:** Sprint 6 (Inpatient — theatre bookings and ICU episodes both hang off an admission)
**Goal:** Surgery scheduling with an OT checklist and staff/case-load assignment; ICU bed-level monitoring flags. Afya Hospital tier only.

## Context

Research this round (see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md` Round 2)
found that Operating Room management in general HIS products centers on surgery scheduling
(date/time/type/checklist) plus staff-shift assignment aligned to case demand — conceptually the
same "resource booking + staff assignment" shape as pos-api's existing Facility/FacilityBooking
hospitality pattern. Reuse that *shape* for the API design (booking header + resource + status
lifecycle), but this is clinically owned by hospital-api, not pos-api.

## Ent Schemas (as shipped)

- `theatre_booking` — `tenant_id`, `outlet_id`, `patient_visit_id`, `patient_id`, `theatre_room`,
  `surgery_type`, `surgeon_id`, `scheduled_at`, `duration_minutes`, `status`
  (awaiting_payment/scheduled/in_progress/completed/cancelled), `checklist` (JSON map — normalized
  rather than a rigid new table per checklist item, per the "additive metadata over new schema"
  preference), `fee_amount` (snapshotted at booking time — `THEATRE_FEE`'s catalog price is nil,
  procedure fees vary too widely to price generically), `started_at`/`completed_at`.
- `icu_episode` — `tenant_id`, `admission_id`, `bed_id` (snapshot at episode start — NOT
  auto-updated on a later Transfer, a documented staff-workflow expectation, not a system
  invariant), `severity_flag` (stable/guarded/critical), `monitoring_notes`, `started_by`,
  `started_at`/`ended_at`. Deliberately no billing fields — see "Billing" below.

## Billing

- Theatre: `THEATRE_FEE` (department=theatre, `requires_prepayment: true`, no fixed price) —
  `theatre.Service.CreateBooking` posts the explicit `fee_amount` via `billing.Service.PostCharge`
  exactly like `lab.Service.CreateOrder` posts a test charge; the booking starts
  `awaiting_payment` and an `activate` endpoint (identical contract to lab's `ActivateIfPaid`)
  flips it to `scheduled` once paid/exempted. Because `PostCharge` became admission-aware in
  Sprint 6, a booking for an admitted patient's visit lands on the admission's own account
  automatically — zero theatre-specific billing code needed for that.
- ICU: **no separate billing path**. An ICU bed lives in a Ward whose `billable_item_code`
  (Sprint 6) can point at a higher day-rate (e.g. `BED_DAY_ICU`); discharge-time billing already
  charges that correctly with zero ICU-specific logic.

## Endpoints (as shipped)

- `POST /{tenant}/hospital/theatre-bookings` — schedule a surgery (rejects a room/time-slot
  conflict).
- `GET /{tenant}/hospital/theatre-bookings?date=YYYY-MM-DD` — schedule view, optionally scoped to
  one calendar day.
- `GET /{tenant}/hospital/theatre-bookings/{id}` — detail.
- `POST /{tenant}/hospital/theatre-bookings/{id}/activate` — mirrors lab's `ActivateIfPaid`.
- `PUT /{tenant}/hospital/theatre-bookings/{id}/checklist` — replace the pre-op checklist map.
- `POST /{tenant}/hospital/theatre-bookings/{id}/start` — scheduled → in_progress.
- `POST /{tenant}/hospital/theatre-bookings/{id}/complete` — in_progress → completed.
- `POST /{tenant}/hospital/theatre-bookings/{id}/cancel` — waives any pending fee charge.
- `POST /{tenant}/hospital/icu-episodes` — start an ICU episode for an active admission (rejects a
  second concurrent episode for the same admission).
- `GET /{tenant}/hospital/icu-episodes?status=active|all` — the board's data source.
- `GET /{tenant}/hospital/icu-episodes/{id}` — detail.
- `PATCH /{tenant}/hospital/icu-episodes/{id}` — update severity/monitoring notes.
- `POST /{tenant}/hospital/icu-episodes/{id}/end` — end episode.

## RBAC

New `PermICUView/Add/Change/Manage` permission codes (Theatre's own `PermTheatreView/Add/Change/
Manage` already existed, pre-seeded ahead of this sprint but never granted to any role — the same
gap Sprint 6 found for `PermInpatientAdd`/`Manage`). Role grants split by real clinical ownership:
Doctor gets full `theatre.*` + ICU view/change; Nurse gets full `icu.*` + theatre view-only;
Manager gets both broadly, matching this role's existing scope.

## Integration Points

- None new — both features consume `admission`/`bed` from Sprint 6, and theatre reuses
  `billing.Service.PostCharge` unchanged.

## Definition of Done

- [x] Theatre scheduling with conflict detection (no double-booking a theatre room/time slot) —
      verified in `internal/integration/theatre_icu_golden_path_test.go`, including that a
      back-to-back booking in the same room and a same-time booking in a DIFFERENT room are both
      correctly ALLOWED (not over-rejected).
- [x] ICU episode lifecycle tied correctly to admission/discharge — one active episode per
      admission enforced; ending/reopening verified.
- [x] Atlas migration generated and applied to the local dev DB. `go build`/`go vet`/`go test`
      clean.

## Next Sprint

Sprint 8 — Blood Bank & Transfusion.
