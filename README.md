# hospital-api (Codevertex Afya)

Hospital & clinic management microservice for the Codevertex platform — consultation, laboratory,
pharmacy/dispensing, inpatient, and billing/insurance workflows for one connected patient record.

**Status:** Sprint-0 scaffold (2026-07-31). Config, logging, Postgres/Redis/NATS wiring, health
endpoints, and JWKS auth middleware are live; there are no domain ent schemas or business logic yet.

See `docs/plan.md` for the full product vision and roadmap, `docs/architecture.md` for the layer
overview and data-ownership boundaries, `docs/integrations.md` for how this service talks to
inventory-api/treasury-api/auth-api/subscriptions-api/notifications-api, and `docs/erd.md` for the
planned entity model.

## Local development

```bash
cp .env.example .env
go run ./cmd/api
# GET http://localhost:4200/healthz
```

## Stack

Go 1.26 · chi v5 · ent v0.14 + Atlas (once schemas land) · PostgreSQL (pgx) · Redis · NATS JetStream
(`shared-events` outbox, aggregate_type `hospital`) · `shared/{httpware,auth-client,cache,service-client,pagination}`.
