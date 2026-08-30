-- Widen "hospital_users".auth_service_user_id uniqueness from platform-wide to per-tenant.
-- A prior version of this schema made auth_service_user_id globally unique (and used it as
-- the row's own primary key), so one auth-service user could only ever have a single
-- HospitalUser row across the entire platform. The composite unique index
-- "hospitaluser_tenant_id_auth_service_user_id" already existed and is untouched by this
-- migration — it is now the ONLY uniqueness constraint on this column, allowing the same
-- auth-service user to hold one HospitalUser row per tenant they belong to.
-- Modify "hospital_users" table
DROP INDEX "hospital_users_auth_service_user_id_key";
DROP INDEX "hospitaluser_auth_service_user_id";
