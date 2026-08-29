-- Create "diagnosis_catalog_defaults" table
CREATE TABLE "diagnosis_catalog_defaults" ("id" uuid NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "category" character varying NULL, "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "diagnosiscatalogdefault_category" to table: "diagnosis_catalog_defaults"
CREATE INDEX "diagnosiscatalogdefault_category" ON "diagnosis_catalog_defaults" ("category");
-- Create index "diagnosiscatalogdefault_code" to table: "diagnosis_catalog_defaults"
CREATE UNIQUE INDEX "diagnosiscatalogdefault_code" ON "diagnosis_catalog_defaults" ("code");
-- Create "diagnosis_catalog_entries" table
CREATE TABLE "diagnosis_catalog_entries" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "code" character varying NOT NULL, "name" character varying NOT NULL, "category" character varying NULL, "is_active" boolean NOT NULL DEFAULT true, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "diagnosiscatalogentry_tenant_id_code" to table: "diagnosis_catalog_entries"
CREATE UNIQUE INDEX "diagnosiscatalogentry_tenant_id_code" ON "diagnosis_catalog_entries" ("tenant_id", "code");
-- Create "examination_records" table
CREATE TABLE "examination_records" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "clinician_id" uuid NOT NULL, "queue_type" character varying NOT NULL DEFAULT 'doctor', "chief_complaint" character varying NULL, "diagnosis_code" character varying NULL, "diagnosis_name" character varying NULL, "notes" character varying NULL, "status" character varying NOT NULL DEFAULT 'in_progress', "examined_at" timestamptz NOT NULL, "completed_at" timestamptz NULL, "visit_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "examination_records_patient_visits_examination_records" FOREIGN KEY ("visit_id") REFERENCES "patient_visits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "examinationrecord_tenant_id_status" to table: "examination_records"
CREATE INDEX "examinationrecord_tenant_id_status" ON "examination_records" ("tenant_id", "status");
-- Create index "examinationrecord_tenant_id_visit_id" to table: "examination_records"
CREATE INDEX "examinationrecord_tenant_id_visit_id" ON "examination_records" ("tenant_id", "visit_id");
