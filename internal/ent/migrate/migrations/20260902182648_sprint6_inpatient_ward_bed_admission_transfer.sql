-- Create "patient_transfers" table
CREATE TABLE "patient_transfers" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "admission_id" uuid NOT NULL, "transfer_type" character varying NOT NULL, "from_ward_id" uuid NOT NULL, "from_bed_id" uuid NOT NULL, "to_ward_id" uuid NULL, "to_bed_id" uuid NULL, "receiving_facility_name" character varying NULL, "referral_id" uuid NULL, "ambulance_booking_id" uuid NULL, "reason" character varying NULL, "transferred_by" uuid NULL, "transferred_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "patienttransfer_tenant_id_admission_id" to table: "patient_transfers"
CREATE INDEX "patienttransfer_tenant_id_admission_id" ON "patient_transfers" ("tenant_id", "admission_id");
-- Create index "patienttransfer_tenant_id_transferred_at" to table: "patient_transfers"
CREATE INDEX "patienttransfer_tenant_id_transferred_at" ON "patient_transfers" ("tenant_id", "transferred_at");
-- Create "wards" table
CREATE TABLE "wards" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "name" character varying NOT NULL, "capacity" bigint NOT NULL DEFAULT 0, "billable_item_code" character varying NULL, "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "ward_tenant_id_outlet_id" to table: "wards"
CREATE INDEX "ward_tenant_id_outlet_id" ON "wards" ("tenant_id", "outlet_id");
-- Create index "ward_tenant_id_outlet_id_name" to table: "wards"
CREATE UNIQUE INDEX "ward_tenant_id_outlet_id_name" ON "wards" ("tenant_id", "outlet_id", "name");
-- Create "beds" table
CREATE TABLE "beds" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "bed_number" character varying NOT NULL, "status" character varying NOT NULL DEFAULT 'available', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "ward_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "beds_wards_beds" FOREIGN KEY ("ward_id") REFERENCES "wards" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "bed_tenant_id_status" to table: "beds"
CREATE INDEX "bed_tenant_id_status" ON "beds" ("tenant_id", "status");
-- Create index "bed_tenant_id_ward_id" to table: "beds"
CREATE INDEX "bed_tenant_id_ward_id" ON "beds" ("tenant_id", "ward_id");
-- Create index "bed_ward_id_bed_number" to table: "beds"
CREATE UNIQUE INDEX "bed_ward_id_bed_number" ON "beds" ("ward_id", "bed_number");
-- Create "admissions" table
CREATE TABLE "admissions" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "patient_id" uuid NOT NULL, "admission_number" character varying NOT NULL, "ward_id" uuid NOT NULL, "status" character varying NOT NULL DEFAULT 'active', "admitted_by" uuid NULL, "admitted_at" timestamptz NOT NULL, "discharged_at" timestamptz NULL, "discharged_by" uuid NULL, "discharge_summary" character varying NULL, "ward_charge_posted" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "bed_id" uuid NOT NULL, "patient_visit_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "admissions_beds_admissions" FOREIGN KEY ("bed_id") REFERENCES "beds" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION, CONSTRAINT "admissions_patient_visits_admissions" FOREIGN KEY ("patient_visit_id") REFERENCES "patient_visits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "admission_tenant_id_admission_number" to table: "admissions"
CREATE UNIQUE INDEX "admission_tenant_id_admission_number" ON "admissions" ("tenant_id", "admission_number");
-- Create index "admission_tenant_id_bed_id" to table: "admissions"
CREATE INDEX "admission_tenant_id_bed_id" ON "admissions" ("tenant_id", "bed_id");
-- Create index "admission_tenant_id_patient_visit_id" to table: "admissions"
CREATE INDEX "admission_tenant_id_patient_visit_id" ON "admissions" ("tenant_id", "patient_visit_id");
-- Create index "admission_tenant_id_status" to table: "admissions"
CREATE INDEX "admission_tenant_id_status" ON "admissions" ("tenant_id", "status");
-- Create index "admission_tenant_id_ward_id" to table: "admissions"
CREATE INDEX "admission_tenant_id_ward_id" ON "admissions" ("tenant_id", "ward_id");
