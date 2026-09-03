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

## Gap audit and Sprint 7.1 candidates (2026-09-02, later the same day)

**Shipped 2026-09-03**: every candidate below landed except the two explicitly deferred by this
section's own recommendation (instrument-count as a dedicated field, and the inventory-api
`AssetReservation` overlap-check gap — both stay exactly as this section already said). The real
WHO 19-item, 3-phase checklist (`sign_in`/`time_out`/`sign_out`) replaced the 5 invented items —
a pure hospital-ui seed-data/UI change (`checklist` was already a free-form `map[string]bool`, no
backend field or migration needed; phase grouping is a key-naming convention, not a schema
change). New `TheatreStaffAssignment` entity (`surgeon_id` kept as-is for backward compat) +
`staffBookingConflict`, extending the existing room-only `hasOverlap` check to also reject a
booking (or a new team-member assignment) whose staff member already has an overlapping booking in
a *different* room — checked on both `CreateBooking` and `AssignStaff`. New `PacuStay` entity
(never reuses `ICUEpisode`; a `to_icu` disposition is a UI/workflow signal to separately start a
real `ICUEpisode`, not an auto-link) and `OperativeNote` entity (one-to-one, create-or-amend on
each `RecordOperativeNote` call). All three surfaced through one `Team / PACU / Op Note` modal on
the theatre schedule page rather than three separate UI entry points, since they're all
booking-scoped clinical actions a theatre nurse/surgeon would reach for from the same place.

A client-facing engineer reviewed the shipped module and flagged that "theatre is also still missing
quite a number of sub modules." This section is a **research-grounded gap audit, proposed design
only** — nothing below is built, and none of it changes a shipped field. Sourcing: the official WHO
Surgical Safety Checklist (read directly from WHO's own published PDF, `who.int`, revised 1/2009,
©WHO 2009 — the exact text is reproduced below, not paraphrased), general OR-scheduling-software
research on multi-resource conflict detection, PACU/recovery-unit literature, and JCAHO/AAAHC
operative-report documentation standards.

### The WHO Surgical Safety Checklist — real content, to replace the 5 made-up items

The shipped `checklist` JSON currently ships 5 items the team invented for this pass (`consent_signed`,
`site_marked`, `anaesthesia_reviewed`, `blood_available`, `equipment_ready`) — not a real standard.
The actual WHO checklist is a named, globally standardized 19-item tool run at three points in every
case. Verbatim from the official WHO document:

**Sign In — before induction of anaesthesia** (with at least nurse and anaesthetist):
1. Has the patient confirmed his/her identity, site, procedure, and consent?
2. Is the site marked? (yes / not applicable)
3. Is the anaesthesia machine and medication check complete?
4. Is the pulse oximeter on the patient and functioning?
5. Does the patient have a known allergy? (no / yes)
6. Does the patient have a difficult airway or aspiration risk? (no / yes — and equipment/assistance
   available)
7. Does the patient have a risk of >500 mL blood loss (7 mL/kg in children)? (no / yes — and two
   IVs/central access and fluids planned)

**Time Out — before skin incision** (with nurse, anaesthetist, and surgeon):
1. Confirm all team members have introduced themselves by name and role.
2. Confirm the patient's name, procedure, and where the incision will be made.
3. Has antibiotic prophylaxis been given within the last 60 minutes? (yes / not applicable)
4. Anticipated critical events — to the surgeon: what are the critical or non-routine steps? How long
   will the case take? What is the anticipated blood loss?
5. To the anaesthetist: are there any patient-specific concerns?
6. To the nursing team: has sterility (including indicator results) been confirmed? Are there
   equipment issues or any concerns?
7. Is essential imaging displayed? (yes / not applicable)

**Sign Out — before the patient leaves the operating room** (with nurse, anaesthetist, and surgeon):
1. Nurse verbally confirms: the name of the procedure; completion of instrument, sponge, and needle
   counts; specimen labelling (read specimen labels aloud, including patient name); whether there are
   any equipment problems to be addressed.
2. To surgeon, anaesthetist, and nurse: what are the key concerns for recovery and management of this
   patient?

WHO's own footer text: *"This checklist is not intended to be comprehensive. Additions and
modifications to fit local practice are encouraged."* — consistent with keeping `checklist` a JSON
map rather than a rigid table, per this codebase's existing "additive metadata over new schema"
preference. **Proposed**: reshape the JSON default from the current flat 5-key map into three
phase-grouped keys (`sign_in`, `time_out`, `sign_out`), each holding the items above as booleans (plus
the few multi-choice items, e.g. "known allergy," as a tri-state or short enum rather than a plain
bool), seeded as the new tenant default, still freely tenant-editable exactly as today.

### Surgical team assignment — recommend a new `TheatreStaffAssignment`-shaped entity

`TheatreBooking.surgeon_id` (shipped) is a single assigned surgeon only. Real theatre teams have
distinct roles — surgeon, one or more assistant surgeons, anaesthetist, scrub nurse, circulating
nurse — each a named responsibility with different downstream needs (staff-conflict checking,
role-specific sign-off on the checklist, staffing rosters). **Recommendation: add a new entity, not a
set of new named fields on `TheatreBooking`.** Named fields (`assistant_surgeon_id`,
`anaesthetist_id`, `scrub_nurse_id`, ...) would hit a wall the moment a case needs two assistant
surgeons, or a role this platform didn't anticipate; a table scales to any team size and role mix
without another migration. **Proposed**: `theatre_staff_assignment` — `id`, `tenant_id`,
`theatre_booking_id`, `staff_user_id`, `role` (enum: `surgeon`|`assistant_surgeon`|`anaesthetist`|
`scrub_nurse`|`circulating_nurse`|`other`), `assigned_at`. Keep `TheatreBooking.surgeon_id` exactly as
shipped for backward compatibility and for the common case of a quick "who's operating" glance/list
query — a booking with no assignment rows can be treated as having exactly one implied `surgeon` row
(synthesized from `surgeon_id` at read time), so nothing that already depends on `surgeon_id` needs to
change. This is additive, not a replacement.

### Conflict detection scope — extend to staff, and flag a real gap in the equipment path

The shipped `hasOverlap` check only guards the room/time slot. General OR-scheduling research
confirms real systems check further: operating rooms require multiple secondary resources (surgeons,
anaesthesiologists), and a well-built system "displays conflicts not only with the primary resource
but also with respect to scheduled utilization of secondary resources" when a case runs over. **Proposed**:
extend the conflict check to also reject a booking whose `surgeon_id` (or any `theatre_staff_assignment`
row in a blocking role, once that entity exists) is already booked in an overlapping window in a
*different* room — the same staff member obviously can't be in two theatres at once. This is a pure
additive extension of the existing overlap query, not a redesign.

**Equipment conflict — a real, confirmed gap to flag back, not fixed here.** If theatre bookings start
reserving specific equipment via inventory-api's `AssetReservation` (see `docs/architecture.md`'s new
Asset Integration section), a direct read of inventory-api's own
`internal/http/handlers/extras_asset_ops.go` `CreateAssetReservation` handler (2026-09-02, this audit)
found it performs **no overlap/time-window check at all** — it unconditionally inserts a new
`pending`-status reservation row regardless of any other reservation already covering that asset for
an overlapping window. hospital-api's own theatre-booking conflict check would need to independently
query existing reservations for the same `asset_id`/time window itself before trusting an
`AssetReservation` as a real double-booking guard — inventory-api does not currently provide that
guarantee. Worth fixing in inventory-api directly at some point (a natural, small addition mirroring
`TheatreBooking`'s own `hasOverlap` shape), but out of scope for this docs-only pass; flagged here so
the concurrent implementation doesn't assume a protection that isn't actually there yet.

### PACU / recovery tracking — recommend a new minimal entity, not `ICUEpisode` reuse

**Question posed**: should post-op recovery be a new entity, reuse `ICUEpisode`, or a new
`TheatreBooking` status? **Recommendation: a new minimal entity.** PACU literature confirms it is a
genuinely distinct clinical stage — "an essential intermediary between the operating theatre and the
Intensive Care Unit," where most patients are NOT critically ill and are stabilized for a short period
before going home, to a ward, or (only sometimes) to the ICU. Reusing `ICUEpisode` would force routine,
low-acuity recoveries onto a `severity_flag`/monitoring model built for genuine critical care, and
would put every post-op patient onto the ICU board regardless of actual severity — a real, misleading
signal for staff scanning that board for who actually needs ICU-level attention. A new `TheatreBooking`
status (e.g. `in_recovery`) was also considered and rejected: the booking's own lifecycle tracks the
*room/procedure* (freed the moment surgery completes, so the room can turn over for the next case),
while PACU tracks the *patient* (who is very much still occupying staff/monitoring attention after the
room is already free) — conflating the two would block room-turnover reporting on an unrelated
patient-recovery timeline. **Proposed**: a new `pacu_stay` entity, mirroring `ICUEpisode`'s own
minimal shape rather than inventing a new pattern — `id`, `tenant_id`, `theatre_booking_id`, `bay_label`,
`admitted_at`, `discharged_at`, `discharge_disposition` (enum: `to_ward`|`to_icu`|`home`|`deceased`),
`monitoring_notes`. If `discharge_disposition` is `to_icu`, a real `ICUEpisode` starts as it does
today — PACU is a short, lower-acuity waypoint, not a replacement for ICU tracking.

### Structured operative notes — recommend a new linked entity, not new `TheatreBooking` fields

JCAHO/AAAHC's own standard operative-report structure (confirmed via direct research) organizes a
report into: pre- and post-procedure diagnoses, the procedure name, indications, intraoperative
findings, specimens sent, anaesthesia type, complications, estimated blood loss, and a detailed
procedure description — plus, separately, documentation of any implants used. This is a long,
free-standing clinical document authored *after* the procedure completes, with its own author/timing,
structurally different from `TheatreBooking`'s own scheduling fields. **Proposed**: a new
`operative_note` entity, one-to-one with a completed booking — `id`, `tenant_id`,
`theatre_booking_id` (unique), `surgeon_id`, `procedure_performed`, `findings`, `complications`,
`estimated_blood_loss_ml`, `implants_used` (text), `specimens_sent` (bool + text description),
`post_op_diagnosis`, `authored_by`, `authored_at`. Additive, does not touch any shipped
`TheatreBooking` field.

### Instrument/sponge/swab count verification — fold into the checklist, don't build a new field

The WHO checklist itself treats this as a single Sign Out confirmation ("completion of instrument,
sponge and needle counts"), not a numeric tally register — the standard itself is a verbal
verification, not a counted ledger. **Recommendation**: keep it exactly where the real checklist puts
it, as one boolean item inside the reshaped `sign_out` JSON section above, rather than inventing a
dedicated counted-items field. A numeric discrepancy log (separate "instruments out" / "instruments
in" counters) is a real thing some facilities keep for medico-legal reasons beyond what WHO's own
checklist specifies, but no research this round found it as a standard baseline expectation — flag as
a possible future refinement only if a specific facility asks for it, not a default recommendation now.

### Asset/equipment wiring — see `docs/architecture.md` — shipped 2026-09-02

Theatre equipment (anaesthesia machines, monitors) and ICU equipment (ventilators) shipped the same
day via `equipment_asset_ids` (a JSON array) on `TheatreBooking`/`ICUEpisode`, plus a
`PUT .../theatre-bookings/{id}/equipment` endpoint and the existing ICU `PATCH` extended to accept
it. `AssetReservation` was deliberately NOT used for this — the two real gaps found in it (no
overlap check, confirmed above; no status-transition HTTP endpoint at all past creation) meant it
could not actually provide the double-booking prevention its name implies. Full design and sourcing
in `docs/architecture.md`'s "Biomedical Equipment / Asset Integration" section; real reservation-
backed equipment conflict prevention is a documented future increment gated on inventory-api fixing
those two gaps first.

## Next Sprint

Sprint 8 — Blood Bank & Transfusion.
