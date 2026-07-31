# Hospital API — Sprint 9: Ambulance & Emergency Dispatch + Asset/Equipment Integration

**Status:** ⏳ Planned
**Depends on:** Sprint 5 (Billing, for ambulance-fee invoicing)
**Goal:** Thin reference integration into logistics-api for ambulance dispatch (no new fleet/dispatch engine), and a read-only surface over inventory-api's existing Asset/AssetMaintenance register as "Biomedical Equipment". Afya Hospital tier.

## Context

Both features in this sprint are **integration work, not new domain engines** — the whole point of
this sprint is to *not* build what already exists elsewhere on the platform. See
`docs/integrations.md` § 1.5 and § 2A for the full ADRs.

## Ent Schemas to Add

- `ambulance_booking` — `patient_visit_id` (nullable — a call may precede patient registration),
  `logistics_task_id`, `pickup_location`, `status`, `fare_amount`. **Reference row only.**
- `ambulance_membership` (optional, can slip to a later phase if time-constrained) — `tenant_id`,
  `crm_contact_id`, `plan_type` (individual/family), `expires_at`.
- **No new schema for assets** — this sprint is 100% integration against inventory-api's existing
  `Asset`/`AssetMaintenance`.

## Endpoints

- `POST /{tenant}/hospital/ambulance-bookings` — creates a `Task` on logistics-api
  (`task_type: "ambulance_dispatch"`), stores the returned `logistics_task_id`.
- `GET /{tenant}/hospital/ambulance-bookings/{id}` — status, proxying live status from logistics-api
  where useful, cached from `logistics.task.assigned`/`logistics.task.completed` events otherwise.
- `GET /{tenant}/hospital/assets` — proxies inventory-api's asset list (read-through, no local cache
  table beyond what `shared/cache` already provides generically).
- `GET /{tenant}/hospital/assets/{id}/maintenance` — proxies inventory-api's maintenance history.

## Integration Points

- logistics-api: `POST /v1/{tenant}/tasks` with `task_type: "ambulance_dispatch"` (additive string
  value, zero logistics-api schema change), subscribe to `logistics.task.assigned`/`.completed`.
- inventory-api: `GET /v1/{tenant}/inventory/assets*` (confirm exact paths against inventory-api's
  own docs when this sprint starts — the asset handlers exist in inventory-api's ent layer but the
  HTTP handler surface should be double-checked, not assumed).
- treasury-api: ambulance fare becomes a billable line on the patient's invoice (Sprint 5's
  checkout), or a recurring membership charge if `ambulance_membership` ships this sprint.

## Definition of Done

- [ ] Ambulance booking creates a real logistics-api task and receives status updates.
- [ ] Ambulance fare (distance-based, via logistics-api's existing `PricingRule`) flows into a
      treasury invoice correctly.
- [ ] Asset/equipment list and maintenance history render correctly from inventory-api's live data —
      confirmed zero local duplication (no `asset` table exists in hospital-api's schema).
- [ ] `go build`/`go vet` clean; no new logistics-api or inventory-api schema/migration was needed
      (confirms the reuse thesis from `docs/integrations.md`).

## Next Sprint

Sprint 10 — Specialized care programmes + KHIS/DHIS2 aggregate reporting.
