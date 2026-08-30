-- Add Tenant.settings — tenant-admin-writable facility operating settings (hospital.config.manage:
-- auto_logout_minutes, default_landing_view, operating_hours). Deliberately a SEPARATE column
-- from metadata (which is fully owned/overwritten by the subscriptions-api sync), so a routine
-- tenant sync can never clobber an admin-set value.
-- Modify "tenants" table
ALTER TABLE "tenants" ADD COLUMN "settings" jsonb NULL;

-- Create "hospital_user_outlets" table — per-user outlet/branch assignment, mirroring
-- inventory-api's UserOutlet / pos-api's StaffOutlet (the fleet's proven pattern). user_id is the
-- LOCAL HospitalUser.ID (consistent with UserRoleAssignment), not the auth-service id. Enforced
-- by internal/http/middleware/outlet_context.go for any caller who cannot access all outlets.
CREATE TABLE "hospital_user_outlets" (
  "id" uuid NOT NULL,
  "tenant_id" uuid NOT NULL,
  "user_id" uuid NOT NULL,
  "outlet_id" uuid NOT NULL,
  "is_home_outlet" boolean NOT NULL DEFAULT false,
  "assigned_by" uuid NULL,
  "assigned_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "hospitaluseroutlet_tenant_id_user_id_outlet_id" ON "hospital_user_outlets" ("tenant_id", "user_id", "outlet_id");
CREATE INDEX "hospitaluseroutlet_tenant_id_outlet_id" ON "hospital_user_outlets" ("tenant_id", "outlet_id");
CREATE INDEX "hospitaluseroutlet_tenant_id_user_id" ON "hospital_user_outlets" ("tenant_id", "user_id");
