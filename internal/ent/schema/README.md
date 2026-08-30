# Ent Schemas

27+ real entities live here, shipped across Sprints 0-5 (Patient/OPD/Triage, Consultation,
Laboratory, Pharmacy/Dispensing, Billing/Insurance, Trinity RBAC/identity). See `docs/erd.md` for
the current entity-relationship model and `docs/sprints/` for what landed in each sprint.

Conventions followed by every schema in this directory:

- One file per entity, `snake_case.go` matching the lowercase entity name.
- `BaseMixin` (id + timestamps) and `TenantMixin` (`tenant_id`) shared mixins, following the pattern
  in `inventory-service/inventory-api/internal/ent/schema/` and
  `library-service/library-api/internal/ent/schema/`.
- After adding/editing a schema: `go generate ./internal/ent/...` then generate an Atlas versioned
  migration (see `feedback_ent_atlas_migrations.md` in the project memory for the full workflow and
  the safe-migration patterns for populated production tables).
- Reference/catalog entities that are the same for every tenant (e.g. `HospitalRole`,
  `HospitalPermission`, `LabTest`, `DiagnosisCatalog`) must NOT carry `tenant_id` by default — see
  `feedback_shared_core_reference_data.md`. A documented nullable-`tenant_id` copy-on-write override
  is the sanctioned exception for genuine per-tenant customization (e.g. `HospitalRole` tenant clones
  — see `docs/plan.md`'s user-management addendum). Tenant-specific business data (Patient,
  PatientVisit, Prescription, HospitalUser, ...) keeps `tenant_id`.
