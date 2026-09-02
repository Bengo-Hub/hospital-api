-- Create "icu_episodes" table
CREATE TABLE "icu_episodes" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "admission_id" uuid NOT NULL, "bed_id" uuid NOT NULL, "severity_flag" character varying NOT NULL DEFAULT 'stable', "monitoring_notes" character varying NULL, "started_by" uuid NULL, "started_at" timestamptz NOT NULL, "ended_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "icuepisode_tenant_id_admission_id" to table: "icu_episodes"
CREATE INDEX "icuepisode_tenant_id_admission_id" ON "icu_episodes" ("tenant_id", "admission_id");
-- Create index "icuepisode_tenant_id_bed_id" to table: "icu_episodes"
CREATE INDEX "icuepisode_tenant_id_bed_id" ON "icu_episodes" ("tenant_id", "bed_id");
-- Create "theatre_bookings" table
CREATE TABLE "theatre_bookings" ("id" uuid NOT NULL, "tenant_id" uuid NOT NULL, "outlet_id" uuid NOT NULL, "patient_visit_id" uuid NOT NULL, "patient_id" uuid NOT NULL, "theatre_room" character varying NOT NULL, "surgery_type" character varying NOT NULL, "surgeon_id" uuid NULL, "scheduled_at" timestamptz NOT NULL, "duration_minutes" bigint NOT NULL DEFAULT 60, "status" character varying NOT NULL DEFAULT 'scheduled', "checklist" jsonb NULL, "fee_amount" double precision NULL, "created_by" uuid NULL, "started_at" timestamptz NULL, "completed_at" timestamptz NULL, "created_at" timestamptz NOT NULL, "updated_at" timestamptz NOT NULL, PRIMARY KEY ("id"));
-- Create index "theatrebooking_tenant_id_patient_visit_id" to table: "theatre_bookings"
CREATE INDEX "theatrebooking_tenant_id_patient_visit_id" ON "theatre_bookings" ("tenant_id", "patient_visit_id");
-- Create index "theatrebooking_tenant_id_status" to table: "theatre_bookings"
CREATE INDEX "theatrebooking_tenant_id_status" ON "theatre_bookings" ("tenant_id", "status");
-- Create index "theatrebooking_tenant_id_theatre_room" to table: "theatre_bookings"
CREATE INDEX "theatrebooking_tenant_id_theatre_room" ON "theatre_bookings" ("tenant_id", "theatre_room");
