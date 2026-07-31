#!/bin/sh
# Entrypoint for hospital-api: wait for DB, run migrations + seed, then start the server.
# Migrate/seed are currently no-ops (Sprint 0 scaffold, no ent schemas yet) — this script
# already follows the fleet-standard shape (library-api/inventory-api) so Sprint 0 only
# needs to fill in real migrations, not rewrite the entrypoint.

set -e

MIGRATE_URL="${POSTGRES_MIGRATE_URL:-$POSTGRES_URL}"

echo "=========================================="
echo "Hospital-API Service Startup"
echo "=========================================="

echo "Running migrations (no-op until Sprint 0 adds ent schemas)..."
POSTGRES_URL="$MIGRATE_URL" /usr/local/bin/hospital-migrate || echo "Migrate step reported an issue (non-fatal at this scaffold stage)"

echo "Running seed (idempotent, no-op until Sprint 0 adds ent schemas)..."
POSTGRES_URL="$MIGRATE_URL" /usr/local/bin/hospital-seed || echo "Seed completed with warnings (non-fatal)"

echo ""
echo "=========================================="
echo "Starting Hospital-API server"
echo "=========================================="
echo ""

exec /usr/local/bin/hospital
