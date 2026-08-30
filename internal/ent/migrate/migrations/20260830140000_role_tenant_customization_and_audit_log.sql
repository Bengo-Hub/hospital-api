-- Add copy-on-write tenant role customization to "hospital_roles": tenant_id (NULL = global)
-- and cloned_from_role_id. The old single role_code unique index is replaced by two partial
-- unique indexes so a tenant's clone can reuse its parent's exact role_code (disjoint scopes:
-- global codes unique where tenant_id IS NULL, per-tenant codes unique where tenant_id IS NOT
-- NULL). Purely additive/widening — all existing seeded rows get tenant_id = NULL, unaffected.
-- Modify "hospital_roles" table
ALTER TABLE "hospital_roles" ADD COLUMN "tenant_id" uuid NULL, ADD COLUMN "cloned_from_role_id" uuid NULL;
DROP INDEX "hospitalrole_role_code";
CREATE INDEX "hospitalrole_tenant_id" ON "hospital_roles" ("tenant_id");
CREATE INDEX "hospitalrole_cloned_from_role_id" ON "hospital_roles" ("cloned_from_role_id");
CREATE UNIQUE INDEX "hospitalrole_role_code" ON "hospital_roles" ("role_code") WHERE ("tenant_id" IS NULL);
CREATE UNIQUE INDEX "hospitalrole_tenant_id_role_code" ON "hospital_roles" ("tenant_id", "role_code") WHERE ("tenant_id" IS NOT NULL);

-- Create "rbac_audit_logs" table — a minimal, additive audit trail scoped to identity/RBAC
-- mutations only (role assigned/changed, role created/customized/edited, user status changed).
-- Deliberately named/scoped narrower than Sprint 12's eventual full compliance-grade audit_log
-- so the two can coexist without a naming collision. No FK edges on purpose — a row must stay
-- inspectable even after its target user/role is later hard-deleted.
CREATE TABLE "rbac_audit_logs" (
  "id" uuid NOT NULL,
  "tenant_id" uuid NOT NULL,
  "actor_user_id" uuid NOT NULL,
  "actor_email" character varying NULL,
  "action" character varying NOT NULL,
  "target_type" character varying NOT NULL,
  "target_id" uuid NOT NULL,
  "before" jsonb NULL,
  "after" jsonb NULL,
  "created_at" timestamptz NOT NULL,
  PRIMARY KEY ("id")
);
CREATE INDEX "rbacauditlog_tenant_id_created_at" ON "rbac_audit_logs" ("tenant_id", "created_at");
CREATE INDEX "rbacauditlog_target_type_target_id" ON "rbac_audit_logs" ("target_type", "target_id");
CREATE INDEX "rbacauditlog_actor_user_id" ON "rbac_audit_logs" ("actor_user_id");
