-- Modify "admissions" table
ALTER TABLE "admissions" ADD COLUMN "discharge_diagnosis" character varying NULL, ADD COLUMN "procedures_performed" character varying NULL, ADD COLUMN "discharge_medications" character varying NULL, ADD COLUMN "follow_up_instructions" character varying NULL, ADD COLUMN "condition_at_discharge" character varying NULL;
-- Modify "beds" table
ALTER TABLE "beds" ADD COLUMN "isolation_precaution" character varying NOT NULL DEFAULT 'none';
-- Modify "wards" table
ALTER TABLE "wards" ADD COLUMN "ward_type" character varying NULL;
-- Create "vitals_chart_entries" table
CREATE TABLE "vitals_chart_entries" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "recorded_by" uuid NOT NULL, "bp_systolic" bigint NULL, "bp_diastolic" bigint NULL, "temperature_celsius" double precision NULL, "pulse_bpm" bigint NULL, "respiration_rate" bigint NULL, "spo2_percent" double precision NULL, "pain_score" bigint NULL, "notes" character varying NULL, "recorded_at" timestamptz NOT NULL, "admission_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "vitals_chart_entries_admissions_vitals_chart_entries" FOREIGN KEY ("admission_id") REFERENCES "admissions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "vitalschartentry_tenant_id_admission_id" to table: "vitals_chart_entries"
CREATE INDEX "vitalschartentry_tenant_id_admission_id" ON "vitals_chart_entries" ("tenant_id", "admission_id");
-- Create "ward_round_notes" table
CREATE TABLE "ward_round_notes" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "clinician_id" uuid NOT NULL, "notes" character varying NOT NULL, "diagnosis_code" character varying NULL, "diagnosis_name" character varying NULL, "recorded_at" timestamptz NOT NULL, "admission_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "ward_round_notes_admissions_ward_round_notes" FOREIGN KEY ("admission_id") REFERENCES "admissions" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "wardroundnote_tenant_id_admission_id" to table: "ward_round_notes"
CREATE INDEX "wardroundnote_tenant_id_admission_id" ON "ward_round_notes" ("tenant_id", "admission_id");
