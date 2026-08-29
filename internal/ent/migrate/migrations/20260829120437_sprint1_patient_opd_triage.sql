-- Create "patients" table
CREATE TABLE "patients" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "mrn" character varying NOT NULL, "full_name" character varying NOT NULL, "dob" timestamptz NULL, "sex" character varying NULL, "phone" character varying NULL, "id_number" character varying NULL, "address" character varying NULL, "next_of_kin" character varying NULL, "allergy_flags" jsonb NULL, "client_registry_id" character varying NULL, "crm_contact_id" uuid NULL, "status" character varying NOT NULL DEFAULT 'active', "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "patient_tenant_id_full_name" to table: "patients"
CREATE INDEX "patient_tenant_id_full_name" ON "patients" ("tenant_id", "full_name");
-- Create index "patient_tenant_id_id_number" to table: "patients"
CREATE INDEX "patient_tenant_id_id_number" ON "patients" ("tenant_id", "id_number");
-- Create index "patient_tenant_id_mrn" to table: "patients"
CREATE UNIQUE INDEX "patient_tenant_id_mrn" ON "patients" ("tenant_id", "mrn");
-- Create index "patient_tenant_id_phone" to table: "patients"
CREATE INDEX "patient_tenant_id_phone" ON "patients" ("tenant_id", "phone");
-- Create "patient_visits" table
CREATE TABLE "patient_visits" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "visit_number" character varying NOT NULL, "visit_type" character varying NOT NULL DEFAULT 'OPD', "status" character varying NOT NULL DEFAULT 'registered', "chief_complaint" character varying NULL, "registered_by" uuid NULL, "checked_in_at" timestamptz NOT NULL, "discharged_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, "patient_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "patient_visits_patients_visits" FOREIGN KEY ("patient_id") REFERENCES "patients" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "patientvisit_tenant_id_outlet_id" to table: "patient_visits"
CREATE INDEX "patientvisit_tenant_id_outlet_id" ON "patient_visits" ("tenant_id", "outlet_id");
-- Create index "patientvisit_tenant_id_patient_id" to table: "patient_visits"
CREATE INDEX "patientvisit_tenant_id_patient_id" ON "patient_visits" ("tenant_id", "patient_id");
-- Create index "patientvisit_tenant_id_status" to table: "patient_visits"
CREATE INDEX "patientvisit_tenant_id_status" ON "patient_visits" ("tenant_id", "status");
-- Create index "patientvisit_tenant_id_visit_number" to table: "patient_visits"
CREATE UNIQUE INDEX "patientvisit_tenant_id_visit_number" ON "patient_visits" ("tenant_id", "visit_number");
-- Create "referrals" table
CREATE TABLE "referrals" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "referred_to" character varying NOT NULL, "reason" character varying NULL, "status" character varying NOT NULL DEFAULT 'pending', "referred_by" uuid NULL, "created_at" timestamptz NOT NULL, "visit_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "referrals_patient_visits_referrals" FOREIGN KEY ("visit_id") REFERENCES "patient_visits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "referral_tenant_id_status" to table: "referrals"
CREATE INDEX "referral_tenant_id_status" ON "referrals" ("tenant_id", "status");
-- Create index "referral_tenant_id_visit_id" to table: "referrals"
CREATE INDEX "referral_tenant_id_visit_id" ON "referrals" ("tenant_id", "visit_id");
-- Create "triage_records" table
CREATE TABLE "triage_records" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "taken_by" uuid NOT NULL, "bp_systolic" bigint NULL, "bp_diastolic" bigint NULL, "temperature_celsius" double precision NULL, "pulse_bpm" bigint NULL, "respiration_rate" bigint NULL, "spo2_percent" double precision NULL, "weight_kg" double precision NULL, "height_cm" double precision NULL, "priority" character varying NULL, "notes" character varying NULL, "taken_at" timestamptz NOT NULL, "visit_id" uuid NOT NULL, PRIMARY KEY ("id"), CONSTRAINT "triage_records_patient_visits_triage_records" FOREIGN KEY ("visit_id") REFERENCES "patient_visits" ("id") ON UPDATE NO ACTION ON DELETE NO ACTION);
-- Create index "triagerecord_tenant_id_visit_id" to table: "triage_records"
CREATE INDEX "triagerecord_tenant_id_visit_id" ON "triage_records" ("tenant_id", "visit_id");
