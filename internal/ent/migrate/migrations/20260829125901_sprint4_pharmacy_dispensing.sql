-- Create "controlled_substance_logs" table
CREATE TABLE "controlled_substance_logs" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "prescription_id" uuid NULL, "item_sku" character varying NOT NULL, "item_name" character varying NOT NULL, "quantity_dispensed" double precision NOT NULL, "dispensed_by" uuid NOT NULL, "patient_name" character varying NULL, "patient_id_number" character varying NULL, "witness_staff_id" uuid NULL, "notes" character varying NULL, "lot_number" character varying NULL, "lot_expiry_date" timestamptz NULL, "dispensed_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "controlledsubstancelog_tenant_id_dispensed_at" to table: "controlled_substance_logs"
CREATE INDEX "controlledsubstancelog_tenant_id_dispensed_at" ON "controlled_substance_logs" ("tenant_id", "dispensed_at");
-- Create index "controlledsubstancelog_tenant_id_prescription_id" to table: "controlled_substance_logs"
CREATE INDEX "controlledsubstancelog_tenant_id_prescription_id" ON "controlled_substance_logs" ("tenant_id", "prescription_id");
-- Create "drug_interaction_checks" table
CREATE TABLE "drug_interaction_checks" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "prescription_id" uuid NULL, "drug_skus" jsonb NULL, "result" character varying NOT NULL DEFAULT 'clear', "details" jsonb NULL, "checked_by" uuid NULL, "checked_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "druginteractioncheck_tenant_id_prescription_id" to table: "drug_interaction_checks"
CREATE INDEX "druginteractioncheck_tenant_id_prescription_id" ON "drug_interaction_checks" ("tenant_id", "prescription_id");
-- Create "prescriptions" table
CREATE TABLE "prescriptions" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "patient_id" uuid NULL, "visit_id" uuid NULL, "examination_id" uuid NULL, "external_facility_name" character varying NULL, "prescription_number" character varying NOT NULL, "prescriber_name" character varying NULL, "prescriber_license" character varying NULL, "patient_name" character varying NULL, "patient_dob" timestamptz NULL, "patient_id_number" character varying NULL, "status" character varying NOT NULL DEFAULT 'pending', "notes" character varying NULL, "dispensed_at" timestamptz NULL, "dispensed_by" uuid NULL, "metadata" jsonb NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "prescription_tenant_id_patient_id" to table: "prescriptions"
CREATE INDEX "prescription_tenant_id_patient_id" ON "prescriptions" ("tenant_id", "patient_id");
-- Create index "prescription_tenant_id_prescription_number" to table: "prescriptions"
CREATE UNIQUE INDEX "prescription_tenant_id_prescription_number" ON "prescriptions" ("tenant_id", "prescription_number");
-- Create index "prescription_tenant_id_status" to table: "prescriptions"
CREATE INDEX "prescription_tenant_id_status" ON "prescriptions" ("tenant_id", "status");
-- Create index "prescription_tenant_id_visit_id" to table: "prescriptions"
CREATE INDEX "prescription_tenant_id_visit_id" ON "prescriptions" ("tenant_id", "visit_id");
-- Create "prescription_lines" table
CREATE TABLE "prescription_lines" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "inventory_item_sku" character varying NULL, "drug_name" character varying NOT NULL, "dosage" character varying NULL, "form" character varying NULL, "instructions" character varying NULL, "quantity_prescribed" double precision NOT NULL, "quantity_dispensed" double precision NOT NULL DEFAULT 0, "unit_price" double precision NOT NULL DEFAULT 0, "lot_number" character varying NULL, "expiry_date" timestamptz NULL, "status" character varying NOT NULL DEFAULT 'pending', "prescription_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "prescription_lines_prescriptions_lines" FOREIGN KEY ("prescription_id") REFERENCES "prescriptions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "prescriptionline_tenant_id_prescription_id" to table: "prescription_lines"
CREATE INDEX "prescriptionline_tenant_id_prescription_id" ON "prescription_lines" ("tenant_id", "prescription_id");
