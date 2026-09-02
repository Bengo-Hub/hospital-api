# hospital-api (Codevertex Afya)

Hospital & clinic management microservice for the Codevertex platform — consultation, laboratory,
pharmacy/dispensing, inpatient, and billing/insurance workflows for one connected patient record.

**Status:** Sprints 0-7 shipped and live in production (Codevertex Afya). Patient/OPD/Triage,
Consultation/Examination, Laboratory, Pharmacy/Dispensing, Billing/Insurance, Inpatient
(ward/bed/admission/transfer/discharge), and Theatre/OT scheduling + ICU critical-care monitoring
are real, working Go code with Trinity Authorization (JWT RBAC + subscription licensing + local
resource RBAC) fully wired. The pos-api pharmacy migration this service absorbed is complete and
decisively removed from pos-api. Sprints 8-13 (Blood Bank onward) are still planned. See
`docs/plan.md` for the current, authoritative status and roadmap — this file is a static entry
point, not the source of truth.

See `docs/plan.md` for the full product vision and roadmap, `docs/architecture.md` for the layer
overview and data-ownership boundaries, `docs/integrations.md` for how this service talks to
inventory-api/treasury-api/auth-api/subscriptions-api/notifications-api, and `docs/erd.md` for the
current entity model.

## Local development

```bash
cp .env.example .env
go run ./cmd/api
# GET http://localhost:4200/healthz
```

## Stack

Go 1.26 · chi v5 · ent v0.14 + Atlas · PostgreSQL (pgx) · Redis · NATS JetStream
(`shared-events` outbox, aggregate_type `hospital`) · `shared/{httpware,auth-client,cache,service-client,pagination}`.
