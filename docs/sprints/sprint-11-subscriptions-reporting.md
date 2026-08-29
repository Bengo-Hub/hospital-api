# Hospital API — Sprint 11: Subscriptions/Licensing + Reporting & Analytics

**Status:** ⏳ Planned
**Depends on:** all prior domain sprints (feature gates need real features to gate)
**Goal:** Wire Trinity Layer 2 (subscriptions-api `service_tag: hospital`) for real, and ship occupancy/revenue/clinical-throughput dashboards.

## Ent Schemas to Add

- None new in hospital-api — subscription plan/tier data lives entirely in subscriptions-api per
  the platform convention (`shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`: "Other services do not
  store plan or entitlement data").
- Optional: `report_snapshot` (cached aggregate for dashboard performance) — not authoritative data,
  purely a materialized-view-style cache, safe to rebuild.

## Tasks

- [ ] Register `service_tag: "hospital"` in subscriptions-api with plans `AFYA_CLINIC`,
      `AFYA_FACILITY`, `AFYA_HOSPITAL`, plus the standalone-chemist configuration from Sprint 4.
- [ ] Feature gates: `inpatient_module`, `in_house_lab`, `insurance_claims`, `theatre_icu`,
      `blood_bank`, `ambulance_dispatch`, `multi_branch`, `specialized_programmes`, `khis_reporting`,
      `api_access`.
- [ ] Wire mutations-only subscription enforcement middleware (matching every sibling service).
- [ ] Occupancy dashboard: bed/theatre/ICU utilization by ward/outlet.
- [ ] Revenue dashboard: **delegates to treasury-api's finance/analytics aggregation** — never
      re-sums payment data locally (per `treasury-analytics-reports-modular.md`'s "finance module =
      SoT for revenue/expense/tax aggregation, delegate never re-implement sums" rule).
- [ ] Clinical-throughput dashboard: patients seen/day, average wait time, lab turnaround time — all
      computed from hospital-api's own owned data.
- [ ] Programme dashboard tiles (ART/TB/Immunization/VMMC/HEI/cancer-screening counts) — a real,
      published field test found Kenya's own automated indicator-reporting mechanism produced
      100% complete/accurate MOH-731 data versus 89%/71% for manual entry (`sprint-10`), a genuine
      evidence-backed value proposition worth surfacing directly in this dashboard's copy, not just
      building the numbers.

## Definition of Done

- [ ] A tenant on the standalone-chemist configuration cannot access Inpatient/Theatre/Blood-Bank
      routes (403 with `subscription_inactive`/`feature not available`, matching the platform's
      standard error shape).
- [ ] Dashboards render real data from at least one seeded demo tenant.
- [ ] `go build`/`go vet` clean.

## Next Sprint

Sprint 12 — Compliance hardening (Kenya DPA).
