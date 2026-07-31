# Hospital API — Sprint 13: Launch + Decisive pos-api Pharmacy Decommission

**Status:** ⏳ Planned
**Depends on:** every prior sprint, and — critically — the per-tenant cutover in `docs/migration-pos-pharmacy.md` Phase C must be complete for **every** tenant before this sprint's decommission step (§ 2 below) runs.
**Goal:** Production readiness for hospital-api, and the final, decisive removal of all pharmacy/clinical logic from pos-api.

## Part 1 — Production Readiness

- [ ] Runbooks: incident response, backup/restore (tenant-scoped, per `feedback_tenant_scoped_backups.md` — never a platform-wide dump), disaster recovery RTO/RPO targets.
- [ ] Load/performance testing against realistic clinic-day volumes (per tier: ~30/day Afya Clinic up to multi-hundred/day Afya Hospital).
- [ ] Full Swagger/OpenAPI coverage (`swag init`, served at `/v1/docs/`, matching every sibling service).
- [ ] `devops-k8s/apps/hospital-api` in production with HA (`minReplicas: 2` + PDB), matching `ha-min-2-pods-and-pdb.md`.
- [ ] Tenant onboarding playbook (data migration checklist, staff training materials).

## Part 2 — pos-api Pharmacy Decommission (`docs/migration-pos-pharmacy.md` Phase D)

**Only run this once every pos-api pharmacy/DAWA tenant has been cut over to hospital-api (Phase C, done per-tenant, not here).**

- [ ] Delete pos-api's clinical/pharmacy ent schemas, handlers, modules, migrations, and frontend
      code (full list in `migration-pos-pharmacy.md` § 2).
- [ ] Generate a new Atlas migration on pos-api that **drops** the pharmacy/clinical tables.
- [ ] Remove `pharmacy`/`dawa` from pos-api's `use_case` enum — valid values become exactly `retail`,
      `hospitality`, `quick_service`, `services`.
- [ ] Update `pos-service/pos-api/docs/{plan,architecture,integrations,erd}.md` to remove every
      trace of pharmacy/clinical ownership.
- [ ] Remove the retired `POWERSUITE_DAWA_*` plan family from subscriptions-api's seed.
- [ ] Update `shared-docs/CROSS-SERVICE-DATA-OWNERSHIP.md`'s hospital-api section to drop the
      "currently owned by pos-api pending migration" qualifier.
- [ ] `go build ./...` + full test suite green on pos-api after removal, before pushing to main
      (per `feedback_workflow_rules.md`).

## Definition of Done

- [ ] Zero pharmacy/clinical code paths remain anywhere in `pos-service/pos-api` or `pos-service/pos-ui`.
- [ ] hospital-api is the platform's sole pharmacy/clinical system of record, verified by grepping
      pos-api's codebase for `Prescription`/`ControlledSubstanceLog`/`Patient` and finding nothing.
- [ ] hospital-api is live in production, HA, monitored, documented, and onboarding real tenants.

## Beyond Sprint 13

Ongoing feature work (additional specialized programmes, deeper analytics, mobile apps, telemedicine)
is tracked as new sprint docs added after this point — this roadmap covers the path to a
feature-complete, production-ready Codevertex Afya v1, not the product's entire future.
