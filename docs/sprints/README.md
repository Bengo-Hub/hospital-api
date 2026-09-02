# Hospital API — Sprint Plans

**Last updated:** 2026-08-29 (Sprints 1, 2, 5-core, 3, 4 shipped this session, in that build order —
see each sprint file's Status header and `.claude/plans/pharmacy-to-hospital-service-migration-2026-08-29.md`
for the full progress log. Originally written 2026-07-31, Round 2, after the expanded
department-catalog research; see `.claude/plans/hospital-service-codevertex-afya-2026-07-31.md`).

| Sprint | Topic | File |
|---|---|---|
| 0 | Foundations | [sprint-0-foundations.md](sprint-0-foundations.md) ✅ shipped |
| 1 | Patient registry, OPD reception/queuing, Triage | [sprint-1-patient-opd-triage.md](sprint-1-patient-opd-triage.md) ✅ shipped 2026-08-29 |
| 2 | Consultation & Examination, Diagnosis Catalogue | [sprint-2-consultation-examination.md](sprint-2-consultation-examination.md) ✅ shipped 2026-08-29 |
| 3 | Laboratory | [sprint-3-laboratory.md](sprint-3-laboratory.md) ✅ shipped 2026-08-29 |
| 4 | Pharmacy & Dispensing (migrated from pos-api) + standalone-chemist config | [sprint-4-pharmacy-dispensing.md](sprint-4-pharmacy-dispensing.md) ✅ core dispensing shipped 2026-08-29 (label-PDF endpoint + route-level module gating still open) |
| 5 | Billing & Insurance | [sprint-5-billing-insurance.md](sprint-5-billing-insurance.md) ✅ core ledger shipped 2026-08-29 (insurance-flow wiring still open) |
| 6 | Inpatient | [sprint-6-inpatient.md](sprint-6-inpatient.md) ✅ shipped 2026-09-02 |
| 7 | Theatre/OT + ICU | [sprint-7-theatre-icu.md](sprint-7-theatre-icu.md) ✅ shipped 2026-09-02 |
| 8 | Blood Bank & Transfusion | [sprint-8-blood-bank.md](sprint-8-blood-bank.md) |
| 9 | Ambulance & Emergency Dispatch + Asset/Equipment integration | [sprint-9-ambulance-assets.md](sprint-9-ambulance-assets.md) |
| 10 | Specialized care programmes + KHIS/DHIS2 reporting | [sprint-10-specialized-programmes-khis.md](sprint-10-specialized-programmes-khis.md) |
| 11 | Subscriptions/licensing + reporting/analytics | [sprint-11-subscriptions-reporting.md](sprint-11-subscriptions-reporting.md) |
| 12 | Compliance hardening (Kenya DPA) | [sprint-12-compliance-hardening.md](sprint-12-compliance-hardening.md) |
| 13 | Launch + decisive pos-api pharmacy decommission | [sprint-13-launch-and-pos-decommission.md](sprint-13-launch-and-pos-decommission.md) |

See `../plan.md` for the high-level phased-roadmap table and `../migration-pos-pharmacy.md` for the
full pos-api pharmacy migration plan referenced by Sprints 4 and 13. See
`../kenyaemr-technical-reference.md` for the KenyaEMR technical audit behind several 2026-08-29
updates across Sprints 3, 5, 10, 12, and 13 (billing/claims validation, the specialized-programmes
expansion, and the compliance-architecture detail).
