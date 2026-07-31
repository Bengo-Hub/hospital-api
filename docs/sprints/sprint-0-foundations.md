# Hospital API — Sprint 0: Foundations

**Status:** ✅ Shipped
**Period:** 2026-07-31
**Last updated:** 2026-07-31
**Goal:** Repo scaffold that `go build`s clean, wires the platform's standard infra (Postgres/Redis/NATS), and proves JWKS auth end to end — with zero domain business logic.

## Context

This is the first commit of the `hospital-service` repo. Nothing else in the roadmap can start
until the standard platform wiring exists and is verified working, mirroring how
`library-service/library-api`'s Phase-1 scaffold was built (see the project memory entry
`project_library_management.md`).

## What Shipped

- `go.mod` — module `github.com/bengobox/hospital-service`, Go 1.26, tagged shared-lib dependencies
  (`httpware`, `shared-auth-client` via the `auth-client` replace directive), no local `replace ../x` paths.
- `internal/config` — env-var configuration (App, HTTP, Postgres, Redis, Events/NATS, Auth/JWKS, Services S2S URLs).
- `internal/shared/logger` — zap structured logger (JSON in prod, console in dev).
- `internal/platform/database` — pgx connection pool.
- `internal/platform/cache` — Redis client.
- `internal/platform/events` — NATS connection + `hospital` JetStream stream (`hospital.>` subjects), matching the uniform `{aggregate_type}.{event_type}` convention in `shared-docs/event-architecture.md`.
- `internal/http/handlers/health.go` — `/healthz` (liveness), `/readyz` (checks Postgres/Redis/NATS), `/metrics` (Prometheus).
- `internal/http/handlers/ping.go` + `internal/http/router` — one JWKS-authenticated placeholder route (`GET /api/v1/{tenant}/hospital/ping`) proving the auth middleware chain.
- `internal/app/app.go` — wires everything above; **no ent client yet** (there are no schemas to generate one from).
- `cmd/{api,migrate,seed}` — `api` runs the server; `migrate`/`seed` are no-op placeholders logging that there's nothing to do yet.
- `Dockerfile` (multi-stage, three binaries) + `scripts/entrypoint.sh`, matching the `library-api`/`inventory-api` shape.
- `docs/{plan,architecture,integrations,erd}.md` — target-state documentation written ahead of the code, so Sprint 1+ has a spec to build against instead of improvising.

## Verification (done)

- `go mod tidy` — resolves clean.
- `go build ./...` — clean, no errors.
- `go vet ./...` — clean.
- `go run ./cmd/api` — starts, `GET /healthz` → `200 {"status":"ok","service":"hospital-api"}`.
- `GET /readyz` → `503` with real dependency failures reported (`postgres: database "hospital" does not exist`, `redis: connection refused`) — confirms the check logic is real, not a stub, since no local Postgres/Redis/NATS was running in this environment.
- `GET /api/v1/demo/hospital/ping` (no token) → `401` — confirms the JWKS auth middleware is correctly wired and rejecting unauthenticated requests, mirroring the `library-api` scaffold's own smoke test.

## Explicitly NOT in this sprint

- No ent schemas, no ent client, no database tables.
- No outbox publisher (needs an `outboxevent` ent schema to exist first).
- No domain modules (Patient, Visit, Triage, ...).
- No local RBAC tables / `/auth/me` endpoint.
- No S2S clients to inventory-api/treasury-api/notifications-api/subscriptions-api (contracts are specified in `docs/integrations.md`, not implemented).
- No Kubernetes deployment yet — a `devops-k8s/apps/hospital-api/{app.yaml,values.yaml}` stub exists locally but is deliberately **not** committed to the `devops-k8s` repo until there is an actual container image to deploy (ArgoCD would otherwise try to sync a non-existent image and fail — the same gotcha hit `library-api`'s rollout).

## Next Sprint

Sprint 1 — Patient registry, OPD reception/queuing, Triage (migrated from `pos-api`'s `Patient`/`PatientVisit`/`TriageRecord`). See `docs/plan.md` for the full phased roadmap.
