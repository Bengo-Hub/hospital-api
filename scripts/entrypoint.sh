#!/bin/sh
# Entrypoint for hospital-api: wait for DB, run migrations + seed, then start the server.

set -e

MIGRATE_URL="${POSTGRES_MIGRATE_URL:-$POSTGRES_URL}"

echo "=========================================="
echo "Hospital-API Service Startup"
echo "=========================================="

echo "Running migrations..."
# Migration failures are FATAL, not swallowed — a silently-skipped migration means the server
# boots against a DB schema the ent code doesn't match, surfacing later as a confusing
# "column does not exist" runtime error instead of a clear startup failure. Real error output
# reaches the pod logs on every attempt (no /dev/null redirect), per the fleet-wide
# entrypoint.sh fix applied everywhere else in 2026-08-24 (this service was missed then).
POSTGRES_URL="$MIGRATE_URL" /usr/local/bin/hospital-migrate

echo "Running seed (idempotent)..."
POSTGRES_URL="$MIGRATE_URL" /usr/local/bin/hospital-seed || echo "Seed completed with warnings (non-fatal)"

echo ""
echo "=========================================="
echo "Starting Hospital-API server"
echo "=========================================="
echo ""

exec /usr/local/bin/hospital
