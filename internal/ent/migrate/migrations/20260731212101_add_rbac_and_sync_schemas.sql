-- Create "tenants" table
CREATE TABLE "tenants" ("id" uuid NOT NULL, "name" character varying NOT NULL, "slug" character varying NOT NULL, "status" character varying NOT NULL DEFAULT 'active', "use_case" character varying NULL, "sync_status" character varying NOT NULL DEFAULT 'synced', "last_sync_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "tenant_slug" to table: "tenants"
CREATE UNIQUE INDEX "tenant_slug" ON "tenants" ("slug");
-- Create index "tenant_status" to table: "tenants"
CREATE INDEX "tenant_status" ON "tenants" ("status");
-- Create index "tenants_slug_key" to table: "tenants"
CREATE UNIQUE INDEX "tenants_slug_key" ON "tenants" ("slug");
-- Create "hospital_users" table
CREATE TABLE "hospital_users" ("id" uuid NOT NULL, "auth_service_user_id" uuid NOT NULL, "email" character varying NOT NULL, "name" character varying NULL, "status" character varying NOT NULL DEFAULT 'active', "sync_status" character varying NOT NULL DEFAULT 'synced', "last_sync_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "tenant_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "hospital_users_tenants_users" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "hospital_users_auth_service_user_id_key" to table: "hospital_users"
CREATE UNIQUE INDEX "hospital_users_auth_service_user_id_key" ON "hospital_users" ("auth_service_user_id");
-- Create index "hospitaluser_auth_service_user_id" to table: "hospital_users"
CREATE UNIQUE INDEX "hospitaluser_auth_service_user_id" ON "hospital_users" ("auth_service_user_id");
-- Create index "hospitaluser_status" to table: "hospital_users"
CREATE INDEX "hospitaluser_status" ON "hospital_users" ("status");
-- Create index "hospitaluser_sync_status" to table: "hospital_users"
CREATE INDEX "hospitaluser_sync_status" ON "hospital_users" ("sync_status");
-- Create index "hospitaluser_tenant_id" to table: "hospital_users"
CREATE INDEX "hospitaluser_tenant_id" ON "hospital_users" ("tenant_id");
-- Create index "hospitaluser_tenant_id_auth_service_user_id" to table: "hospital_users"
CREATE UNIQUE INDEX "hospitaluser_tenant_id_auth_service_user_id" ON "hospital_users" ("tenant_id", "auth_service_user_id");
-- Create "outlets" table
CREATE TABLE "outlets" ("id" uuid NOT NULL, "tenant_slug" character varying NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "address_json" jsonb NULL, "status" character varying NOT NULL DEFAULT 'active', "use_case" character varying NULL, "is_hq" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "tenant_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "outlets_tenants_outlets" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "outlet_tenant_id_code" to table: "outlets"
CREATE UNIQUE INDEX "outlet_tenant_id_code" ON "outlets" ("tenant_id", "code");
-- Create index "outlet_tenant_slug" to table: "outlets"
CREATE INDEX "outlet_tenant_slug" ON "outlets" ("tenant_slug");
-- Create "hospital_permissions" table
CREATE TABLE "hospital_permissions" ("id" uuid NOT NULL, "permission_code" character varying NOT NULL, "name" character varying NOT NULL, "module" character varying NOT NULL, "action" character varying NOT NULL, "resource" character varying NULL, "description" text NULL, "created_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "hospital_permissions_permission_code_key" to table: "hospital_permissions"
CREATE UNIQUE INDEX "hospital_permissions_permission_code_key" ON "hospital_permissions" ("permission_code");
-- Create index "hospitalpermission_action" to table: "hospital_permissions"
CREATE INDEX "hospitalpermission_action" ON "hospital_permissions" ("action");
-- Create index "hospitalpermission_module" to table: "hospital_permissions"
CREATE INDEX "hospitalpermission_module" ON "hospital_permissions" ("module");
-- Create index "hospitalpermission_module_action" to table: "hospital_permissions"
CREATE INDEX "hospitalpermission_module_action" ON "hospital_permissions" ("module", "action");
-- Create index "hospitalpermission_permission_code" to table: "hospital_permissions"
CREATE UNIQUE INDEX "hospitalpermission_permission_code" ON "hospital_permissions" ("permission_code");
-- Create "hospital_roles" table
CREATE TABLE "hospital_roles" ("id" uuid NOT NULL, "role_code" character varying NOT NULL, "name" character varying NOT NULL, "description" text NULL, "is_system_role" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "hospital_roles_role_code_key" to table: "hospital_roles"
CREATE UNIQUE INDEX "hospital_roles_role_code_key" ON "hospital_roles" ("role_code");
-- Create index "hospitalrole_is_system_role" to table: "hospital_roles"
CREATE INDEX "hospitalrole_is_system_role" ON "hospital_roles" ("is_system_role");
-- Create index "hospitalrole_role_code" to table: "hospital_roles"
CREATE UNIQUE INDEX "hospitalrole_role_code" ON "hospital_roles" ("role_code");
-- Create "role_permissions" table
CREATE TABLE "role_permissions" ("id" bigint NOT NULL GENERATED BY DEFAULT AS IDENTITY, "role_id" uuid NOT NULL, "permission_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "role_permissions_hospital_permissions_permission" FOREIGN KEY ("permission_id") REFERENCES "hospital_permissions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT "role_permissions_hospital_roles_role" FOREIGN KEY ("role_id") REFERENCES "hospital_roles" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "rolepermission_permission_id" to table: "role_permissions"
CREATE INDEX "rolepermission_permission_id" ON "role_permissions" ("permission_id");
-- Create index "rolepermission_role_id" to table: "role_permissions"
CREATE INDEX "rolepermission_role_id" ON "role_permissions" ("role_id");
-- Create index "rolepermission_role_id_permission_id" to table: "role_permissions"
CREATE UNIQUE INDEX "rolepermission_role_id_permission_id" ON "role_permissions" ("role_id", "permission_id");
-- Create "user_role_assignments" table
CREATE TABLE "user_role_assignments" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "assigned_by" uuid NOT NULL, "assigned_at" timestamptz NOT NULL, "expires_at" timestamptz NULL, "user_id" uuid NOT NULL, "role_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "user_role_assignments_hospital_roles_role" FOREIGN KEY ("role_id") REFERENCES "hospital_roles" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT "user_role_assignments_hospital_users_user" FOREIGN KEY ("user_id") REFERENCES "hospital_users" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "userroleassignment_expires_at" to table: "user_role_assignments"
CREATE INDEX "userroleassignment_expires_at" ON "user_role_assignments" ("expires_at");
-- Create index "userroleassignment_role_id" to table: "user_role_assignments"
CREATE INDEX "userroleassignment_role_id" ON "user_role_assignments" ("role_id");
-- Create index "userroleassignment_tenant_id" to table: "user_role_assignments"
CREATE INDEX "userroleassignment_tenant_id" ON "user_role_assignments" ("tenant_id");
-- Create index "userroleassignment_tenant_id_user_id_role_id" to table: "user_role_assignments"
CREATE UNIQUE INDEX "userroleassignment_tenant_id_user_id_role_id" ON "user_role_assignments" ("tenant_id", "user_id", "role_id");
-- Create index "userroleassignment_user_id" to table: "user_role_assignments"
CREATE INDEX "userroleassignment_user_id" ON "user_role_assignments" ("user_id");
