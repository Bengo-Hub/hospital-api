-- Create "billable_item_catalogs" table
CREATE TABLE "billable_item_catalogs" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "department" character varying NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "price" double precision NULL, "applies_to" character varying NOT NULL DEFAULT 'all', "requires_prepayment" boolean NOT NULL DEFAULT false, "collection_mode" character varying NOT NULL DEFAULT 'billing_queue', "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "billableitemcatalog_tenant_id_code" to table: "billable_item_catalogs"
CREATE UNIQUE INDEX "billableitemcatalog_tenant_id_code" ON "billable_item_catalogs" ("tenant_id", "code");
-- Create index "billableitemcatalog_tenant_id_department" to table: "billable_item_catalogs"
CREATE INDEX "billableitemcatalog_tenant_id_department" ON "billable_item_catalogs" ("tenant_id", "department");
-- Create "patient_accounts" table
CREATE TABLE "patient_accounts" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "patient_id" uuid NOT NULL, "visit_id" uuid NULL, "admission_id" uuid NULL, "status" character varying NOT NULL DEFAULT 'open', "total_charged" double precision NOT NULL DEFAULT 0, "total_paid" double precision NOT NULL DEFAULT 0, "balance" double precision NOT NULL DEFAULT 0, "settlement_required_before" character varying NOT NULL DEFAULT 'nothing', "next_of_kin_id" uuid NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "patientaccount_tenant_id_patient_id" to table: "patient_accounts"
CREATE INDEX "patientaccount_tenant_id_patient_id" ON "patient_accounts" ("tenant_id", "patient_id");
-- Create index "patientaccount_tenant_id_status" to table: "patient_accounts"
CREATE INDEX "patientaccount_tenant_id_status" ON "patient_accounts" ("tenant_id", "status");
-- Create index "patientaccount_tenant_id_visit_id" to table: "patient_accounts"
CREATE INDEX "patientaccount_tenant_id_visit_id" ON "patient_accounts" ("tenant_id", "visit_id");
-- Create "patient_next_of_kins" table
CREATE TABLE "patient_next_of_kins" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "patient_id" uuid NOT NULL, "name" character varying NOT NULL, "phone" character varying NULL, "relationship" character varying NULL, "id_number" character varying NULL, "is_primary" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "patientnextofkin_tenant_id_patient_id" to table: "patient_next_of_kins"
CREATE INDEX "patientnextofkin_tenant_id_patient_id" ON "patient_next_of_kins" ("tenant_id", "patient_id");
-- Create "billable_charges" table
CREATE TABLE "billable_charges" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "billable_item_id" uuid NULL, "source_module" character varying NOT NULL, "source_reference_id" uuid NULL, "description" character varying NOT NULL, "amount" double precision NOT NULL, "status" character varying NOT NULL DEFAULT 'pending', "treasury_invoice_id" uuid NULL, "treasury_payment_intent_id" uuid NULL, "created_by_user_id" uuid NULL, "paid_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "patient_account_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "billable_charges_patient_accounts_charges" FOREIGN KEY ("patient_account_id") REFERENCES "patient_accounts" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "billablecharge_tenant_id_patient_account_id" to table: "billable_charges"
CREATE INDEX "billablecharge_tenant_id_patient_account_id" ON "billable_charges" ("tenant_id", "patient_account_id");
-- Create index "billablecharge_tenant_id_source_module" to table: "billable_charges"
CREATE INDEX "billablecharge_tenant_id_source_module" ON "billable_charges" ("tenant_id", "source_module");
-- Create index "billablecharge_tenant_id_status" to table: "billable_charges"
CREATE INDEX "billablecharge_tenant_id_status" ON "billable_charges" ("tenant_id", "status");
