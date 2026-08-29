-- Create "lab_test_catalog_defaults" table
CREATE TABLE "lab_test_catalog_defaults" ("id" uuid NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "specimen_type" character varying NULL, "reference_range" character varying NULL, "unit" character varying NULL, "turnaround_hours" bigint NULL, "price" double precision NOT NULL DEFAULT 0, "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "labtestcatalogdefault_code" to table: "lab_test_catalog_defaults"
CREATE UNIQUE INDEX "labtestcatalogdefault_code" ON "lab_test_catalog_defaults" ("code");
-- Create "lab_test_catalog_entries" table
CREATE TABLE "lab_test_catalog_entries" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "specimen_type" character varying NULL, "reference_range" character varying NULL, "unit" character varying NULL, "turnaround_hours" bigint NULL, "price" double precision NOT NULL DEFAULT 0, "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "labtestcatalogentry_tenant_id_code" to table: "lab_test_catalog_entries"
CREATE UNIQUE INDEX "labtestcatalogentry_tenant_id_code" ON "lab_test_catalog_entries" ("tenant_id", "code");
-- Create "lab_orders" table
CREATE TABLE "lab_orders" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "examination_id" uuid NULL, "ordered_by" uuid NOT NULL, "status" character varying NOT NULL DEFAULT 'requested', "notes" character varying NULL, "ordered_at" timestamptz NOT NULL, "completed_at" timestamptz NULL, "visit_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "lab_orders_patient_visits_lab_orders" FOREIGN KEY ("visit_id") REFERENCES "patient_visits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "laborder_tenant_id_status" to table: "lab_orders"
CREATE INDEX "laborder_tenant_id_status" ON "lab_orders" ("tenant_id", "status");
-- Create index "laborder_tenant_id_visit_id" to table: "lab_orders"
CREATE INDEX "laborder_tenant_id_visit_id" ON "lab_orders" ("tenant_id", "visit_id");
-- Create "lab_order_lines" table
CREATE TABLE "lab_order_lines" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "test_code" character varying NOT NULL, "test_name" character varying NOT NULL, "price" double precision NOT NULL DEFAULT 0, "specimen_type" character varying NULL, "result_value" character varying NULL, "unit" character varying NULL, "reference_range" character varying NULL, "flag" character varying NOT NULL DEFAULT 'pending', "notes" character varying NULL, "resulted_by" uuid NULL, "resulted_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "lab_order_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "lab_order_lines_lab_orders_lines" FOREIGN KEY ("lab_order_id") REFERENCES "lab_orders" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "laborderline_tenant_id_flag" to table: "lab_order_lines"
CREATE INDEX "laborderline_tenant_id_flag" ON "lab_order_lines" ("tenant_id", "flag");
-- Create index "laborderline_tenant_id_lab_order_id" to table: "lab_order_lines"
CREATE INDEX "laborderline_tenant_id_lab_order_id" ON "lab_order_lines" ("tenant_id", "lab_order_id");
