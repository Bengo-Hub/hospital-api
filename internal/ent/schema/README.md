# Ent Schemas — Sprint 0 Placeholder

No ent schemas exist yet. Sprint 0 (see `docs/sprints/sprint-0-foundations.md`) adds the
first entities (`Patient`, `PatientVisit`) here, following the pattern in
`inventory-service/inventory-api/internal/ent/schema/` and
`library-service/library-api/internal/ent/schema/`:

- One file per entity, `snake_case.go` matching the lowercase entity name.
- `BaseMixin` (id + timestamps) and `TenantMixin` (`tenant_id`) shared mixins, copied from
  `library-service/library-api/internal/ent/schema/mixins.go`.
- After adding/editing a schema: `go generate ./internal/ent/...` then generate an Atlas
  versioned migration (see `feedback_ent_atlas_migrations.md` in the project memory).
- Reference/catalog entities that are the same for every tenant (e.g. a default `LabTest`
  or `DiagnosisCatalog` set) must NOT carry `tenant_id` — see
  `feedback_shared_core_reference_data.md`. Tenant-specific business data (Patient,
  PatientVisit, Prescription, ...) keeps `tenant_id`.
